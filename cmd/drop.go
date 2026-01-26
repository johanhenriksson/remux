package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	if !IsWorktree(worktreePath) {
		return fmt.Errorf("not in a git worktree")
	}

	// Check for uncommitted changes
	if HasUncommittedChanges(worktreePath) {
		return fmt.Errorf("worktree has uncommitted changes, aborting")
	}

	// Get the main repo path
	mainRepo, err := getMainRepoPath(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to find main repository: %w", err)
	}

	// Remove the worktree using git
	removeCmd := exec.Command("git", "-C", mainRepo, "worktree", "remove", worktreePath)
	removeCmd.Stderr = os.Stderr
	if err := removeCmd.Run(); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// IsWorktree checks if the given path is a git worktree (not the main repo).
func IsWorktree(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	// In a worktree, .git is a file; in the main repo, it's a directory
	return !info.IsDir()
}

// HasUncommittedChanges checks if there are uncommitted changes in the worktree.
func HasUncommittedChanges(path string) bool {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return true // Assume changes if we can't check
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// getMainRepoPath returns the path to the main repository from a worktree.
func getMainRepoPath(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// git-common-dir returns the .git directory of the main repo
	gitDir := strings.TrimSpace(string(out))
	// Return the parent of .git
	return filepath.Dir(gitDir), nil
}
