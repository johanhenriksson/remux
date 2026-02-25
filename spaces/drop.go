package spaces

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/tmux"
)

// Drop removes a git worktree at the given path and unregisters it.
// Returns an error if the path is not a worktree or has uncommitted changes (unless force is true).
func Drop(worktreePath string, force bool) error {
	if !git.IsWorktree(worktreePath) {
		return fmt.Errorf("not in a git worktree")
	}

	if !force && git.HasUncommittedChanges(worktreePath) {
		return fmt.Errorf("worktree has uncommitted changes, use --force to drop anyway")
	}

	mainRepo, err := git.GetMainRepoPath(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to find main repository: %w", err)
	}

	spaceName := filepath.Base(worktreePath)

	// Ignore SIGHUP so we survive if we're running inside the tmux session being killed
	signal.Ignore(syscall.SIGHUP)

	// Kill the tmux session first so that processes (e.g. servers) started by tabs
	// are stopped before on_drop hooks run (e.g. dropping a database)
	tmux.KillSession(spaceName)

	// Run on_drop hooks before removal (abort on failure)
	// If space isn't registered, skip hooks but continue with removal
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

	return nil
}
