package spaces

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
)

var nonSlugChars = regexp.MustCompile(`[^a-zA-Z0-9-]+`)
var repeatedHyphens = regexp.MustCompile(`-{2,}`)

// SlugifyBranch converts a git branch name into a flat, filesystem-safe slug.
// Non-alphanumeric characters (except hyphens) are replaced with hyphens,
// repeated hyphens are collapsed, and leading/trailing hyphens are trimmed.
func SlugifyBranch(branch string) string {
	s := nonSlugChars.ReplaceAllString(branch, "-")
	s = repeatedHyphens.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// CreateOptions contains the parameters for creating a new space.
type CreateOptions struct {
	RepoRoot            string // Git repository root
	DestDir             string // Destination directory for worktrees
	BranchName          string // Name of the branch to create
	BaseBranch          string // Base branch to create from (empty = current HEAD)
	ReuseExistingBranch bool   // If true, reuse existing branch instead of erroring
}

// Create creates a git worktree and registers it as a space.
// If the branch doesn't exist, it creates a new one.
// If the branch exists and ReuseExistingBranch is true, it reuses it.
// Returns the worktree path on success.
func Create(opts CreateOptions) (string, error) {
	repoName := filepath.Base(opts.RepoRoot)
	spaceName := fmt.Sprintf("%s-%s", repoName, SlugifyBranch(opts.BranchName))
	worktreePath := filepath.Join(opts.DestDir, spaceName)

	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree directory already exists: %s", worktreePath)
	}

	branchExists := git.BranchExists(opts.RepoRoot, opts.BranchName)
	createdBranch := false

	if branchExists && !opts.ReuseExistingBranch {
		return "", fmt.Errorf("branch %q already exists", opts.BranchName)
	}

	if !branchExists {
		var err error
		if opts.BaseBranch != "" {
			err = git.CreateBranchFrom(opts.RepoRoot, opts.BranchName, opts.BaseBranch)
		} else {
			err = git.CreateBranch(opts.RepoRoot, opts.BranchName)
		}
		if err != nil {
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
	unlock, err := registry.Lock(opts.DestDir)
	if err != nil {
		return "", fmt.Errorf("failed to lock registry: %w", err)
	}
	defer unlock()

	reg, err := registry.Load(opts.DestDir)
	if err != nil {
		return "", fmt.Errorf("failed to load registry: %w", err)
	}
	reg.Add(filepath.Base(worktreePath), opts.BranchName, worktreePath, reg.AllocatePort(), opts.RepoRoot)
	if err := reg.Save(opts.DestDir); err != nil {
		return "", fmt.Errorf("failed to save registry: %w", err)
	}

	// Run on_create hooks (warn on failure, don't abort)
	if space, err := Open(worktreePath); err == nil {
		space.RunOnCreate()
	}

	return worktreePath, nil
}
