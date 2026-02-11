package spaces

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
)

// CreateOptions contains the parameters for creating a new space.
type CreateOptions struct {
	RepoRoot            string // Git repository root
	DestDir             string // Destination directory for worktrees
	BranchName          string // Name of the branch to create
	ReuseExistingBranch bool   // If true, reuse existing branch instead of erroring
}

// Create creates a git worktree and registers it as a space.
// If the branch doesn't exist, it creates a new one.
// If the branch exists and ReuseExistingBranch is true, it reuses it.
// Returns the worktree path on success.
func Create(opts CreateOptions) (string, error) {
	repoName := filepath.Base(opts.RepoRoot)
	worktreePath := filepath.Join(opts.DestDir, fmt.Sprintf("%s-%s", repoName, opts.BranchName))

	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree directory already exists: %s", worktreePath)
	}

	branchExists := git.BranchExists(opts.RepoRoot, opts.BranchName)
	createdBranch := false

	if branchExists && !opts.ReuseExistingBranch {
		return "", fmt.Errorf("branch %q already exists", opts.BranchName)
	}

	if !branchExists {
		if err := git.CreateBranch(opts.RepoRoot, opts.BranchName); err != nil {
			return "", fmt.Errorf("failed to create branch: %w", err)
		}
		createdBranch = true
	}

	if err := git.AddWorktree(opts.RepoRoot, worktreePath, opts.BranchName); err != nil {
		if createdBranch {
			_ = git.DeleteBranch(opts.RepoRoot, opts.BranchName)
		}
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	// Register the new space
	reg, err := registry.Load(opts.DestDir)
	if err == nil {
		reg.Add(filepath.Base(worktreePath), worktreePath, reg.AllocatePort(), opts.RepoRoot)
		_ = reg.Save(opts.DestDir)
	}

	// Run on_create hooks (warn on failure, don't abort)
	if space, err := Open(worktreePath); err == nil {
		space.RunOnCreate()
	}

	return worktreePath, nil
}
