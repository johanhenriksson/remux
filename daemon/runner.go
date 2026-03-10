package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
	"github.com/johanhenriksson/remux/tmux"
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

// LaunchAgent starts a claude agent in a tmux window and returns a channel
// that receives the result when the agent completes.
func LaunchAgent(ctx context.Context, issue Issue, agentCmd, prompt, workspacePath string) (<-chan RunResult, error) {
	session := filepath.Base(workspacePath)
	window := "claude-" + issue.Identifier

	// Ensure tmux session exists
	if !tmux.SessionExists(session) {
		if err := tmux.NewSessionDetached(session, workspacePath, nil); err != nil {
			return nil, fmt.Errorf("create tmux session: %w", err)
		}
	}

	// Create a new window for the agent
	if err := tmux.NewWindow(session, workspacePath, window); err != nil {
		return nil, fmt.Errorf("create tmux window: %w", err)
	}

	// Write prompt to file in the workspace
	promptPath := filepath.Join(workspacePath, ".remux-prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return nil, fmt.Errorf("write prompt file: %w", err)
	}

	// Launch the agent
	cmd := fmt.Sprintf(`%s -p "$(cat .remux-prompt.md)" ; echo __REMUX_DONE_$?__`, agentCmd)
	if err := tmux.SendKeys(session, window, cmd); err != nil {
		return nil, fmt.Errorf("send agent command: %w", err)
	}

	resultCh := make(chan RunResult, 1)
	go monitorAgent(ctx, session, window, issue.ID, resultCh)
	return resultCh, nil
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

func monitorAgent(ctx context.Context, session, window, issueID string, resultCh chan<- RunResult) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = tmux.KillWindow(session, window)
			resultCh <- RunResult{IssueID: issueID, Success: false, Err: ctx.Err()}
			return
		case <-ticker.C:
			content, err := tmux.CapturePane(session, window)
			if err != nil {
				continue
			}
			if strings.Contains(content, "__REMUX_DONE_0__") {
				resultCh <- RunResult{IssueID: issueID, Success: true}
				return
			}
			if containsDoneMarker(content) {
				resultCh <- RunResult{IssueID: issueID, Success: false, Err: fmt.Errorf("agent exited with non-zero status")}
				return
			}
		}
	}
}

func containsDoneMarker(content string) bool {
	// Look for __REMUX_DONE_N__ where N is not 0
	idx := strings.Index(content, "__REMUX_DONE_")
	if idx < 0 {
		return false
	}
	rest := content[idx+len("__REMUX_DONE_"):]
	endIdx := strings.Index(rest, "__")
	if endIdx < 0 {
		return false
	}
	code := rest[:endIdx]
	return code != "0"
}

func issueBranch(issue Issue) string {
	if issue.BranchName != "" {
		return issue.BranchName
	}
	return strings.ToLower(issue.Identifier)
}
