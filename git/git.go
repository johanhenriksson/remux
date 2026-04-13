package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindRoot returns the root of the current git repository.
func FindRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// BranchExists checks if a branch exists in the repository.
func BranchExists(repoRoot, name string) bool {
	cmd := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return cmd.Run() == nil
}

// DefaultBranch returns the repository's default branch name. It first tries
// refs/remotes/origin/HEAD; if that symbolic ref is unset, it falls back to
// probing common defaults (main, master) on the origin remote.
func DefaultBranch(repoRoot string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		return strings.TrimPrefix(ref, "origin/"), nil
	}

	for _, candidate := range []string{"main", "master"} {
		if remoteBranchExists(repoRoot, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not determine default branch: origin/HEAD unset and no common default (main, master) found on origin")
}

// remoteBranchExists checks whether refs/remotes/origin/<name> exists locally.
func remoteBranchExists(repoRoot, name string) bool {
	cmd := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+name)
	return cmd.Run() == nil
}

// ResolveBase returns a ref usable as a base for creating new branches: the
// local branch name if it exists, otherwise the origin/-prefixed remote ref.
func ResolveBase(repoRoot, branch string) string {
	if BranchExists(repoRoot, branch) {
		return branch
	}
	return "origin/" + branch
}

// run runs a git command in the specified repository.
func run(repoRoot string, args ...string) error {
	allArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.Command("git", allArgs...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Pull fast-forwards the current branch to match the remote.
func Pull(repoRoot string) error {
	return run(repoRoot, "pull", "--ff-only")
}

// FetchBranch fetches a specific branch from origin and updates the local ref
// to match. Creates the local branch if it doesn't exist. Returns an error if
// the branch doesn't exist on the remote or the fast-forward fails.
func FetchBranch(repoRoot, branch string) error {
	return run(repoRoot, "fetch", "origin", branch+":"+branch)
}

// CreateBranch creates a new branch at the current HEAD.
func CreateBranch(repoRoot, name string) error {
	return run(repoRoot, "branch", name)
}

// CreateBranchFrom creates a new branch from a specific base branch.
func CreateBranchFrom(repoRoot, name, base string) error {
	return run(repoRoot, "branch", name, base)
}

// DeleteBranch deletes a branch.
func DeleteBranch(repoRoot, name string) error {
	return run(repoRoot, "branch", "-d", name)
}

// AddWorktree creates a new worktree for the given branch.
func AddWorktree(repoRoot, path, branch string) error {
	return run(repoRoot, "worktree", "add", path, branch)
}

// RemoveWorktree removes a worktree.
func RemoveWorktree(repoRoot, worktreePath string) error {
	return run(repoRoot, "worktree", "remove", "--force", worktreePath)
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

// GetMainRepoPath returns the path to the main repository from a worktree.
func GetMainRepoPath(worktreePath string) (string, error) {
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
