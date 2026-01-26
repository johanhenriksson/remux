package git

import (
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

// Run runs a git command in the specified repository.
func Run(repoRoot string, args ...string) error {
	allArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.Command("git", allArgs...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
