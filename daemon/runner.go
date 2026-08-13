package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
)

// EnsureWorkspace creates or reuses a workspace for the given issue.
// If baseBranch is non-empty, new branches are created from it instead of HEAD.
// Returns the worktree path.
func EnsureWorkspace(issue Issue, repoRoot, destDir, baseBranch string) (string, error) {
	branch := issueBranch(issue)

	reg, err := registry.Load(destDir)
	if err != nil {
		return "", fmt.Errorf("load registry: %w", err)
	}

	if entry := reg.GetByBranch(branch); entry != nil {
		if _, err := os.Stat(entry.Path); err == nil {
			return entry.Path, nil
		}
	}

	if err := git.Pull(repoRoot); err != nil {
		log.Printf("[%s] git pull: %v (continuing with current HEAD)", issue.Identifier, err)
	}

	if baseBranch != "" {
		if err := git.FetchBranch(repoRoot, baseBranch); err != nil {
			log.Printf("[%s] fetch base branch %q: %v (continuing with local ref)", issue.Identifier, baseBranch, err)
		}
	}

	path, err := spaces.Create(spaces.CreateOptions{
		RepoRoot:            repoRoot,
		DestDir:             destDir,
		BranchName:          branch,
		BaseBranch:          baseBranch,
		ReuseExistingBranch: true,
	})
	if err != nil {
		return "", fmt.Errorf("create workspace: %w", err)
	}
	return path, nil
}

// EnsureSession opens the tmux session for the workspace (detached) so that
// configured tabs (dev servers, etc.) are running. Safe to call if session
// already exists.
func EnsureSession(workspacePath, destDir string, envVars map[string]string) error {
	name := filepath.Base(workspacePath)
	return spaces.OpenSession(spaces.OpenSessionOptions{
		DestDir: destDir,
		Name:    name,
		Detach:  true,
		EnvVars: envVars,
	})
}

type agentKind string

const (
	agentKindClaude agentKind = "claude"
	agentKindCodex  agentKind = "codex"
)

type agentInvocation struct {
	kind  agentKind
	parts []string
}

func buildAgentInvocation(agentCmd string, issue Issue, prompt, sessionID, sessionName string) (agentInvocation, error) {
	parts, err := splitCommandLine(agentCmd)
	if err != nil {
		return agentInvocation{}, fmt.Errorf("parse agent command: %w", err)
	}
	if len(parts) == 0 {
		return agentInvocation{}, fmt.Errorf("agent command is empty")
	}

	if filepath.Base(parts[0]) == "codex" {
		return agentInvocation{
			kind:  agentKindCodex,
			parts: codexAgentArgs(parts, prompt),
		}, nil
	}

	return agentInvocation{
		kind:  agentKindClaude,
		parts: claudeAgentArgs(parts, issue, prompt, sessionID, sessionName),
	}, nil
}

func claudeAgentArgs(parts []string, issue Issue, prompt, sessionID, sessionName string) []string {
	out := append([]string(nil), parts...)
	out = append(out, "--output-format", "stream-json", "--verbose", "--remote-control", issue.Identifier)
	if sessionID != "" {
		out = append(out, "--session-id", sessionID)
	}
	if sessionName != "" {
		out = append(out, "--name", sessionName)
	}
	if hasLabel(issue.Labels, "max") {
		out = append(out, "--effort", "max")
	}
	return append(out, "-p", prompt)
}

func codexAgentArgs(parts []string, prompt string) []string {
	out := append([]string(nil), parts...)
	if !hasCodexExecSubcommand(out) {
		out = append(out, "exec")
	}
	if !hasArg(out, "--json") {
		out = append(out, "--json")
	}
	return append(out, "--", prompt)
}

func hasCodexExecSubcommand(parts []string) bool {
	for _, part := range parts[1:] {
		if part == "exec" || part == "e" {
			return true
		}
	}
	return false
}

func hasArg(parts []string, arg string) bool {
	for _, part := range parts {
		if part == arg {
			return true
		}
	}
	return false
}

func splitCommandLine(command string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	escaped := false
	started := false

	flush := func() {
		args = append(args, b.String())
		b.Reset()
		started = false
	}

	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			started = true
			continue
		}

		switch r {
		case '\t', '\n', '\r', ' ':
			if started {
				flush()
			}
		case '\'', '"':
			quote = r
			started = true
		default:
			b.WriteRune(r)
			started = true
		}
	}

	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if started {
		flush()
	}
	return args, nil
}

// LaunchAgent runs an agent as a subprocess in the workspace directory.
// Parses JSONL output for logging. Returns a channel that receives the
// result when the process exits. If logger is non-nil, text output is streamed
// to the Linear comment.
func LaunchAgent(ctx context.Context, issue Issue, agentCmd, prompt, workspacePath, sessionID, sessionName string, idleTimeout time.Duration, logger *CommentLogger) (<-chan RunResult, error) {
	promptPath := filepath.Join(workspacePath, ".remux-prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return nil, fmt.Errorf("write prompt file: %w", err)
	}

	invocation, err := buildAgentInvocation(agentCmd, issue, prompt, sessionID, sessionName)
	if err != nil {
		return nil, err
	}

	idleCtx, idleCancel := context.WithCancelCause(ctx)

	cmd := exec.CommandContext(idleCtx, invocation.parts[0], invocation.parts[1:]...)
	cmd.Dir = workspacePath
	cmd.Stderr = os.Stderr
	cmd.Env = agentEnv(workspacePath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		idleCancel(nil)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		idleCancel(nil)
		return nil, fmt.Errorf("start agent: %w", err)
	}

	log.Printf("[%s] agent process started (pid %d)", issue.Identifier, cmd.Process.Pid)

	var idleTimer *time.Timer
	if idleTimeout > 0 {
		idleTimer = time.AfterFunc(idleTimeout, func() {
			log.Printf("[%s] agent idle for %s, aborting", issue.Identifier, idleTimeout)
			idleCancel(fmt.Errorf("idle timeout (%s)", idleTimeout))
		})
	}

	resultCh := make(chan RunResult, 1)
	go func() {
		defer idleCancel(nil)

		prefix := fmt.Sprintf("[%s]", issue.Identifier)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		for scanner.Scan() {
			if idleTimer != nil {
				idleTimer.Reset(idleTimeout)
			}
			processStreamLine(prefix, scanner.Bytes(), invocation.kind, logger)
		}

		if idleTimer != nil {
			idleTimer.Stop()
		}

		if logger != nil {
			logger.Close()
		}

		err := cmd.Wait()
		if idleCtx.Err() != nil {
			cause := context.Cause(idleCtx)
			resultCh <- RunResult{IssueID: issue.ID, Success: false, Err: cause}
		} else if err != nil {
			resultCh <- RunResult{IssueID: issue.ID, Success: false, Err: fmt.Errorf("agent exited: %w", err)}
		} else {
			resultCh <- RunResult{IssueID: issue.ID, Success: true}
		}
	}()

	return resultCh, nil
}

// CommentLogger accumulates agent text output and debounce-flushes it to a Linear comment.
type CommentLogger struct {
	client    *LinearClient
	commentID string
	header    string

	mu      sync.Mutex
	lines   []string
	timer   *time.Timer
	pending bool
}

func NewCommentLogger(client *LinearClient, commentID, header string) *CommentLogger {
	return &CommentLogger{
		client:    client,
		commentID: commentID,
		header:    header,
	}
}

func (cl *CommentLogger) Append(text string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	cl.lines = append(cl.lines, text)

	if !cl.pending {
		cl.pending = true
		cl.timer = time.AfterFunc(5*time.Second, cl.flush)
	}
}

func (cl *CommentLogger) flush() {
	cl.mu.Lock()
	cl.pending = false
	body := cl.buildBody()
	cl.mu.Unlock()

	if err := cl.client.UpdateComment(cl.commentID, body); err != nil {
		log.Printf("comment update: %v", err)
	}
}

func (cl *CommentLogger) Close() {
	cl.mu.Lock()
	if cl.timer != nil {
		cl.timer.Stop()
	}
	cl.pending = false
	body := cl.buildBody()
	cl.mu.Unlock()

	if err := cl.client.UpdateComment(cl.commentID, body); err != nil {
		log.Printf("comment update (final): %v", err)
	}
}

func (cl *CommentLogger) buildBody() string {
	if len(cl.lines) == 0 {
		return cl.header
	}
	return cl.header + "\n\n---\n### Agent Log\n" + strings.Join(cl.lines, "\n")
}

// streamEvent is the minimal structure for parsing stream-json lines.
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	// result fields
	IsError    bool    `json:"is_error"`
	DurationMs int     `json:"duration_ms"`
	NumTurns   int     `json:"num_turns"`
	TotalCost  float64 `json:"total_cost_usd"`
	StopReason string  `json:"stop_reason"`

	// assistant message fields
	Message *assistantMessage `json:"message"`
}

type assistantMessage struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

func processStreamLine(prefix string, line []byte, kind agentKind, logger *CommentLogger) {
	if kind == agentKindCodex {
		processCodexStreamLine(prefix, line, logger)
		return
	}
	processClaudeStreamLine(prefix, line, logger)
}

func processClaudeStreamLine(prefix string, line []byte, logger *CommentLogger) {
	var event streamEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return
	}

	switch event.Type {
	case "assistant":
		if event.Message == nil {
			return
		}
		for _, block := range event.Message.Content {
			switch block.Type {
			case "tool_use":
				log.Printf("%s tool: %s", prefix, block.Name)
			case "text":
				if text := firstLine(block.Text); text != "" {
					log.Printf("%s text: %s", prefix, text)
				}
				if logger != nil && strings.TrimSpace(block.Text) != "" {
					logger.Append(block.Text)
				}
			}
		}

	case "result":
		if event.IsError {
			log.Printf("%s result: error after %d turns (%.1fs, $%.4f)",
				prefix, event.NumTurns, float64(event.DurationMs)/1000, event.TotalCost)
		} else {
			log.Printf("%s result: %s after %d turns (%.1fs, $%.4f)",
				prefix, event.StopReason, event.NumTurns, float64(event.DurationMs)/1000, event.TotalCost)
		}
	}
}

func processCodexStreamLine(prefix string, line []byte, logger *CommentLogger) {
	var event map[string]any
	if err := json.Unmarshal(line, &event); err != nil {
		return
	}
	event = unwrapCodexEvent(event)

	for _, text := range codexEventTexts(event) {
		if first := firstLine(text); first != "" {
			log.Printf("%s text: %s", prefix, first)
		}
		if logger != nil && strings.TrimSpace(text) != "" {
			logger.Append(text)
		}
	}

	if name := codexEventToolName(event); name != "" {
		log.Printf("%s tool: %s", prefix, name)
	}

	if codexEventIsResult(event) {
		log.Printf("%s result: %s", prefix, codexEventResult(event))
	}
}

func unwrapCodexEvent(event map[string]any) map[string]any {
	for _, key := range []string{"event", "msg", "payload"} {
		if nested, ok := mapValue(event, key); ok {
			if _, hasType := nested["type"].(string); hasType {
				return nested
			}
		}
	}
	return event
}

func codexEventTexts(event map[string]any) []string {
	var texts []string
	eventType, _ := stringValue(event, "type")
	if eventType == "agent_message" || eventType == "assistant_message" || eventType == "message" {
		appendStringFields(&texts, event, "message", "text")
	}

	item, ok := mapValue(event, "item")
	if !ok || !codexItemIsAssistantMessage(item) {
		return texts
	}

	appendStringFields(&texts, item, "message", "text")
	if content, ok := item["content"].([]any); ok {
		for _, raw := range content {
			block, ok := raw.(map[string]any)
			if !ok || !codexContentIsText(block) {
				continue
			}
			appendStringFields(&texts, block, "text")
		}
	}
	return texts
}

func codexItemIsAssistantMessage(item map[string]any) bool {
	role, _ := stringValue(item, "role")
	itemType, _ := stringValue(item, "type")
	return role == "assistant" || itemType == "assistant_message" || itemType == "agent_message" || (role == "" && itemType == "message")
}

func codexContentIsText(block map[string]any) bool {
	blockType, ok := stringValue(block, "type")
	return !ok || strings.Contains(blockType, "text") || blockType == "message"
}

func appendStringFields(out *[]string, m map[string]any, keys ...string) {
	for _, key := range keys {
		if s, ok := stringValue(m, key); ok && strings.TrimSpace(s) != "" {
			*out = append(*out, s)
		}
	}
}

func codexEventToolName(event map[string]any) string {
	eventType, _ := stringValue(event, "type")
	if strings.Contains(eventType, "tool") || strings.Contains(eventType, "exec") || strings.Contains(eventType, "function") {
		if name := firstStringValue(event, "name", "tool_name", "command", "cmd", "parsed_cmd"); name != "" {
			return name
		}
	}

	item, ok := mapValue(event, "item")
	if !ok {
		return ""
	}
	itemType, _ := stringValue(item, "type")
	if !strings.Contains(itemType, "tool") && !strings.Contains(itemType, "exec") && !strings.Contains(itemType, "function") {
		return ""
	}
	return firstStringValue(item, "name", "tool_name", "command", "cmd", "parsed_cmd")
}

func codexEventIsResult(event map[string]any) bool {
	eventType, _ := stringValue(event, "type")
	switch eventType {
	case "result", "completed", "turn.completed", "turn_complete", "turn.finished", "turn_finished":
		return true
	default:
		return false
	}
}

func codexEventResult(event map[string]any) string {
	if status := firstStringValue(event, "status", "stop_reason", "outcome"); status != "" {
		return status
	}
	if code, ok := numberValue(event, "exit_code"); ok {
		if code == 0 {
			return "exit 0"
		}
		return fmt.Sprintf("exit %.0f", code)
	}
	return "completed"
}

func firstStringValue(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := stringValue(m, key); ok && s != "" {
			return s
		}
	}
	return ""
}

func stringValue(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func numberValue(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

func mapValue(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	nested, ok := v.(map[string]any)
	return nested, ok
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

// CleanupWorkspace removes the workspace for an issue.
func CleanupWorkspace(issue Issue, repoRoot, destDir string) {
	branch := issueBranch(issue)
	repoName := filepath.Base(repoRoot)
	spaceName := fmt.Sprintf("%s-%s", repoName, spaces.SlugifyBranch(branch))
	worktreePath := filepath.Join(destDir, spaceName)

	if err := spaces.Drop(worktreePath, true); err != nil {
		log.Printf("[%s] cleanup workspace: %v", issue.Identifier, err)
	}
}

// agentEnv returns the environment for the agent subprocess,
// merging the current process env with .remux.yaml env vars.
func agentEnv(workspacePath string) []string {
	env := os.Environ()
	space, err := spaces.Open(workspacePath)
	if err != nil {
		log.Printf("open space for env: %v (using inherited env)", err)
		return env
	}
	resolved, err := space.ResolveEnv()
	if err != nil {
		log.Printf("resolve space env: %v (using inherited env)", err)
		return env
	}
	for key, value := range resolved {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}

// parentBranch returns the branch name of the issue's parent, creating the
// local branch from origin or the repo's default branch if necessary. Returns
// empty string if the issue has no parent.
func parentBranch(issue Issue, repoRoot string) (string, error) {
	if issue.Parent == nil {
		return "", nil
	}
	branch := branchNameFor(issue.Parent.BranchName, issue.Parent.Identifier)

	if git.BranchExists(repoRoot, branch) {
		if err := git.FetchBranch(repoRoot, branch); err != nil {
			log.Printf("[%s] fetch parent branch %q: %v (continuing with local ref)", issue.Identifier, branch, err)
		}
		return branch, nil
	}

	if err := git.FetchBranch(repoRoot, branch); err == nil {
		return branch, nil
	}

	defaultBranch, err := git.DefaultBranch(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolve default branch: %w", err)
	}
	base := git.ResolveBase(repoRoot, defaultBranch)

	log.Printf("[%s] creating parent branch %q from %q", issue.Identifier, branch, base)
	if err := git.CreateBranchFrom(repoRoot, branch, base); err != nil {
		return "", fmt.Errorf("create parent branch %q from %q: %w", branch, base, err)
	}
	return branch, nil
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, target) {
			return true
		}
	}
	return false
}

var ticketPattern = regexp.MustCompile(`^(.*?[a-zA-Z]+-\d+)`)

func branchNameFor(branchName, identifier string) string {
	if branchName != "" {
		if m := ticketPattern.FindString(branchName); m != "" {
			return m
		}
	}
	return strings.ToLower(identifier)
}

func issueBranch(issue Issue) string {
	return branchNameFor(issue.BranchName, issue.Identifier)
}
