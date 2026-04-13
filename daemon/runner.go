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

// LaunchAgent runs a claude agent as a subprocess in the workspace directory.
// Parses stream-json output for logging. Returns a channel that receives the
// result when the process exits. If logger is non-nil, text output is streamed
// to the Linear comment.
func LaunchAgent(ctx context.Context, issue Issue, agentCmd, prompt, workspacePath, sessionID, sessionName string, idleTimeout time.Duration, logger *CommentLogger) (<-chan RunResult, error) {
	promptPath := filepath.Join(workspacePath, ".remux-prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return nil, fmt.Errorf("write prompt file: %w", err)
	}

	parts := strings.Fields(agentCmd)
	parts = append(parts, "--output-format", "stream-json", "--verbose", "--remote-control", issue.Identifier)
	if sessionID != "" {
		parts = append(parts, "--session-id", sessionID)
	}
	if sessionName != "" {
		parts = append(parts, "--name", sessionName)
	}
	if hasLabel(issue.Labels, "max") {
		parts = append(parts, "--effort", "max")
	}
	parts = append(parts, "-p", prompt)

	idleCtx, idleCancel := context.WithCancelCause(ctx)

	cmd := exec.CommandContext(idleCtx, parts[0], parts[1:]...)
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
			processStreamLine(prefix, scanner.Bytes(), logger)
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

func processStreamLine(prefix string, line []byte, logger *CommentLogger) {
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
// local branch from origin or master if necessary. Returns empty string if
// the issue has no parent.
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

	log.Printf("[%s] creating parent branch %q from master", issue.Identifier, branch)
	if err := git.CreateBranchFrom(repoRoot, branch, "master"); err != nil {
		return "", fmt.Errorf("create parent branch %q from master: %w", branch, err)
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
