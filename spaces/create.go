package spaces

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/automo/git"
)

// CreateOptions contains the parameters for creating a new space.
type CreateOptions struct {
	RepoRoot   string // Git repository root
	DestDir    string // Destination directory for worktrees
	BranchName string // Name of the branch to create
}

// Create creates a new git branch and worktree, and registers it.
// Returns the worktree path on success.
func Create(opts CreateOptions) (string, error) {
	repoName := filepath.Base(opts.RepoRoot)

	if git.BranchExists(opts.RepoRoot, opts.BranchName) {
		return "", fmt.Errorf("branch %q already exists", opts.BranchName)
	}

	worktreePath := filepath.Join(opts.DestDir, fmt.Sprintf("%s-%s", repoName, opts.BranchName))

	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree directory already exists: %s", worktreePath)
	}

	if err := git.CreateBranch(opts.RepoRoot, opts.BranchName); err != nil {
		return "", fmt.Errorf("failed to create branch: %w", err)
	}

	if err := git.AddWorktree(opts.RepoRoot, worktreePath, opts.BranchName); err != nil {
		_ = git.DeleteBranch(opts.RepoRoot, opts.BranchName)
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	// Register the new space
	reg, err := Load(opts.DestDir)
	if err == nil {
		reg.Add(filepath.Base(worktreePath), worktreePath)
		_ = reg.Save(opts.DestDir)
	}

	return worktreePath, nil
}
