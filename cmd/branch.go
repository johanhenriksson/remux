package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/automo/git"
	"github.com/spf13/cobra"
)

var destDir string

var branchCmd = &cobra.Command{
	Use:   "branch <name>",
	Short: "Create a new branch and worktree",
	Args:  cobra.ExactArgs(1),
	RunE:  runBranch,
}

func init() {
	rootCmd.AddCommand(branchCmd)
	branchCmd.Flags().StringVarP(&destDir, "dest", "d", "", "destination directory for worktrees (default: ~/at)")
}

// BranchOptions contains the parameters for creating a branch and worktree.
type BranchOptions struct {
	RepoRoot   string // Git repository root
	DestDir    string // Worktree destination directory
	BranchName string // Name of the branch to create
}

// CreateBranch creates a new git branch and worktree.
// Returns the worktree path on success.
func CreateBranch(opts BranchOptions) (string, error) {
	// Get repo directory name
	repoName := filepath.Base(opts.RepoRoot)

	// Check if branch already exists
	if git.BranchExists(opts.RepoRoot, opts.BranchName) {
		return "", fmt.Errorf("branch %q already exists", opts.BranchName)
	}

	// Build worktree path
	worktreePath := filepath.Join(opts.DestDir, fmt.Sprintf("%s-%s", repoName, opts.BranchName))

	// Check if worktree path already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree directory already exists: %s", worktreePath)
	}

	// Create branch from current HEAD
	if err := git.Run(opts.RepoRoot, "branch", opts.BranchName); err != nil {
		return "", fmt.Errorf("failed to create branch: %w", err)
	}

	// Create worktree
	if err := git.Run(opts.RepoRoot, "worktree", "add", worktreePath, opts.BranchName); err != nil {
		// Clean up branch if worktree creation fails
		_ = git.Run(opts.RepoRoot, "branch", "-d", opts.BranchName)
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	return worktreePath, nil
}

func runBranch(cmd *cobra.Command, args []string) error {
	branchName := args[0]

	// Find git repo root
	repoRoot, err := git.FindRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Resolve destination directory
	dest := destDir
	if dest == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dest = filepath.Join(homeDir, "at")
	}

	// Create branch and worktree
	worktreePath, err := CreateBranch(BranchOptions{
		RepoRoot:   repoRoot,
		DestDir:    dest,
		BranchName: branchName,
	})
	if err != nil {
		return err
	}

	// Print worktree path (pipeable)
	fmt.Println(worktreePath)
	return nil
}
