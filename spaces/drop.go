package spaces

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/tmux"
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

	// Run on_drop hooks before removal (abort on failure)
	// If space isn't registered, skip hooks but continue with removal
	spaceName := filepath.Base(worktreePath)
	if space, err := Open(worktreePath); err == nil {
		if err := space.RunOnDrop(); err != nil {
			return err
		}
	}

	if err := git.RemoveWorktree(mainRepo, worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	if err := os.RemoveAll(worktreePath); err != nil {
		return fmt.Errorf("failed to remove directory: %w", err)
	}

	// Unregister the space
	destDir := filepath.Dir(worktreePath)
	reg, err := registry.Load(destDir)
	if err == nil {
		reg.Remove(spaceName)
		_ = reg.Save(destDir)
	}

	tmux.KillSession(spaceName)

	return nil
}
