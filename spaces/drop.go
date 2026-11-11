package spaces

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/johanhenriksson/remux/backend"
	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
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

	// Ignore SIGHUP so we survive if we're running inside the session being closed
	signal.Ignore(syscall.SIGHUP)

	// If space isn't registered, skip hooks but still close a session by name
	// and continue with removal
	space, openErr := Open(worktreePath)
	var b backend.Backend
	if openErr == nil {
		b, err = space.Backend()
	} else {
		b, err = newBackend("")
	}
	if err != nil {
		return err
	}

	// Close the session first so that processes (e.g. servers) started by tabs
	// are stopped before on_drop hooks run (e.g. dropping a database)
	bs := backend.Space{Name: spaceName, Path: worktreePath, RepoRoot: mainRepo}
	if err := b.Close(bs); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to close %s session: %v\n", b.Name(), err)
	}

	// Run on_drop hooks before removal (abort on failure)
	if openErr == nil {
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
	unlock, err := registry.Lock(destDir)
	if err != nil {
		return fmt.Errorf("failed to lock registry: %w", err)
	}
	defer unlock()

	reg, err := registry.Load(destDir)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}
	reg.Remove(spaceName)
	if err := reg.Save(destDir); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	return nil
}
