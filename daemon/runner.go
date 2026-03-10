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

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
)

// EnsureWorkspace creates or reuses a workspace for the given issue.
// Returns the worktree path.
func EnsureWorkspace(issue Issue, repoRoot, destDir string) (string, error) {
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

	path, err := spaces.Create(spaces.CreateOptions{
		RepoRoot:            repoRoot,
		DestDir:             destDir,
		BranchName:          branch,
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
func EnsureSession(workspacePath, destDir string) error {
	name := filepath.Base(workspacePath)
	return spaces.OpenSession(spaces.OpenSessionOptions{
		DestDir: destDir,
		Name:    name,
		Detach:  true,
	})
}

// LaunchAgent runs a claude agent as a subprocess in the workspace directory.
// Parses stream-json output for logging. Returns a channel that receives the
// result when the process exits.
func LaunchAgent(ctx context.Context, issue Issue, agentCmd, prompt, workspacePath string) (<-chan RunResult, error) {
	// Write prompt to file for reference
	promptPath := filepath.Join(workspacePath, ".remux-prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return nil, fmt.Errorf("write prompt file: %w", err)
	}

	// Build command with stream-json output
	parts := strings.Fields(agentCmd)
	parts = append(parts, "--output-format", "stream-json", "--verbose", "-p", prompt)

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = workspacePath
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	log.Printf("[%s] agent process started (pid %d)", issue.Identifier, cmd.Process.Pid)

	resultCh := make(chan RunResult, 1)
	go func() {
		prefix := fmt.Sprintf("[%s]", issue.Identifier)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer

		for scanner.Scan() {
			processStreamLine(prefix, scanner.Bytes())
		}

		err := cmd.Wait()
		if ctx.Err() != nil {
			resultCh <- RunResult{IssueID: issue.ID, Success: false, Err: ctx.Err()}
		} else if err != nil {
			resultCh <- RunResult{IssueID: issue.ID, Success: false, Err: fmt.Errorf("agent exited: %w", err)}
		} else {
			resultCh <- RunResult{IssueID: issue.ID, Success: true}
		}
	}()

	return resultCh, nil
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
	Name  string `json:"name"`  // tool_use name
	Input any    `json:"input"` // tool_use input
}

func processStreamLine(prefix string, line []byte) {
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
				// Log first line of text as a summary
				if text := firstLine(block.Text); text != "" {
					log.Printf("%s text: %s", prefix, text)
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

var ticketPattern = regexp.MustCompile(`^(.*?[a-zA-Z]+-\d+)`)

func issueBranch(issue Issue) string {
	if issue.BranchName != "" {
		if m := ticketPattern.FindString(issue.BranchName); m != "" {
			return m
		}
	}
	return strings.ToLower(issue.Identifier)
}
