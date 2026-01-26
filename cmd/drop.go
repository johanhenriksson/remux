package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/johanhenriksson/automo/git"
	"github.com/johanhenriksson/automo/tmux"
	"github.com/spf13/cobra"
)

var dropCmd = &cobra.Command{
	Use:   "drop",
	Short: "Remove the current worktree and its directory",
	Args:  cobra.NoArgs,
	RunE:  runDrop,
}

func init() {
	rootCmd.AddCommand(dropCmd)
}

func runDrop(cmd *cobra.Command, args []string) error {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	if err := DropWorktree(cwd); err != nil {
		return err
	}

	fmt.Printf("Removed worktree: %s\n", filepath.Base(cwd))
	return nil
}

// DropWorktree removes a git worktree at the given path.
// Returns an error if the path is not a worktree or has uncommitted changes.
func DropWorktree(worktreePath string) error {
	// Check if we're in a worktree
	if !git.IsWorktree(worktreePath) {
		return fmt.Errorf("not in a git worktree")
	}

	// Check for uncommitted changes
	if git.HasUncommittedChanges(worktreePath) {
		return fmt.Errorf("worktree has uncommitted changes, aborting")
	}

	// Get the main repo path
	mainRepo, err := git.GetMainRepoPath(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to find main repository: %w", err)
	}

	// Remove the worktree using git
	removeCmd := exec.Command("git", "-C", mainRepo, "worktree", "remove", worktreePath)
	removeCmd.Stderr = os.Stderr
	if err := removeCmd.Run(); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	// Ensure the directory is removed
	if err := os.RemoveAll(worktreePath); err != nil {
		return fmt.Errorf("failed to remove directory: %w", err)
	}

	// Kill matching tmux session if it exists
	tmux.KillSession(filepath.Base(worktreePath))

	return nil
}
