package spaces

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/automo/git"
	"github.com/johanhenriksson/automo/tmux"
)

// Drop removes a git worktree at the given path and unregisters it.
// Returns an error if the path is not a worktree or has uncommitted changes.
func Drop(worktreePath string) error {
	if !git.IsWorktree(worktreePath) {
		return fmt.Errorf("not in a git worktree")
	}

	if git.HasUncommittedChanges(worktreePath) {
		return fmt.Errorf("worktree has uncommitted changes, aborting")
	}

	mainRepo, err := git.GetMainRepoPath(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to find main repository: %w", err)
	}

	if err := git.RemoveWorktree(mainRepo, worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	if err := os.RemoveAll(worktreePath); err != nil {
		return fmt.Errorf("failed to remove directory: %w", err)
	}

	spaceName := filepath.Base(worktreePath)
	destDir := filepath.Dir(worktreePath)

	// Unregister the space
	reg, err := Load(destDir)
	if err == nil {
		reg.Remove(spaceName)
		_ = reg.Save(destDir)
	}

	tmux.KillSession(spaceName)

	return nil
}
