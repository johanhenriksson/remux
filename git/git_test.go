package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/remux/git"
)

func TestGit(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Git Suite")
}

var _ = Describe("Git", func() {
	var (
		mainRepoDir string
		worktreeDir string
		destDir     string
	)

	BeforeEach(func() {
		var err error

		// Create temp directory for main git repo
		mainRepoDir, err = os.MkdirTemp("", "test-main-repo-*")
		Expect(err).NotTo(HaveOccurred())

		// Create temp directory for worktrees
		destDir, err = os.MkdirTemp("", "test-dest-*")
		Expect(err).NotTo(HaveOccurred())

		// Initialize main git repo
		runGitCmd(mainRepoDir, "init")
		runGitCmd(mainRepoDir, "config", "user.email", "test@test.com")
		runGitCmd(mainRepoDir, "config", "user.name", "Test User")

		// Create initial commit
		testFile := filepath.Join(mainRepoDir, "README.md")
		err = os.WriteFile(testFile, []byte("# Test"), 0644)
		Expect(err).NotTo(HaveOccurred())
		runGitCmd(mainRepoDir, "add", ".")
		runGitCmd(mainRepoDir, "commit", "-m", "Initial commit")

		// Create a branch and worktree
		worktreeDir = filepath.Join(destDir, "test-worktree")
		runGitCmd(mainRepoDir, "branch", "test-branch")
		runGitCmd(mainRepoDir, "worktree", "add", worktreeDir, "test-branch")
	})

	AfterEach(func() {
		os.RemoveAll(mainRepoDir)
		os.RemoveAll(destDir)
	})

	Describe("IsWorktree", func() {
		It("returns true for a worktree directory", func() {
			Expect(git.IsWorktree(worktreeDir)).To(BeTrue())
		})

		It("returns false for the main repo", func() {
			Expect(git.IsWorktree(mainRepoDir)).To(BeFalse())
		})

		It("returns false for a non-git directory", func() {
			nonGitDir, err := os.MkdirTemp("", "non-git-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(nonGitDir)

			Expect(git.IsWorktree(nonGitDir)).To(BeFalse())
		})
	})

	Describe("HasUncommittedChanges", func() {
		It("returns false for a clean worktree", func() {
			Expect(git.HasUncommittedChanges(worktreeDir)).To(BeFalse())
		})

		It("returns true when there are uncommitted changes", func() {
			testFile := filepath.Join(worktreeDir, "uncommitted.txt")
			err := os.WriteFile(testFile, []byte("uncommitted"), 0644)
			Expect(err).NotTo(HaveOccurred())

			Expect(git.HasUncommittedChanges(worktreeDir)).To(BeTrue())
		})
	})

	Describe("BranchExists", func() {
		It("returns true for an existing branch", func() {
			Expect(git.BranchExists(mainRepoDir, "test-branch")).To(BeTrue())
		})

		It("returns false for a non-existent branch", func() {
			Expect(git.BranchExists(mainRepoDir, "non-existent")).To(BeFalse())
		})
	})

	Describe("GetMainRepoPath", func() {
		It("returns the main repo path from a worktree", func() {
			path, err := git.GetMainRepoPath(worktreeDir)
			Expect(err).NotTo(HaveOccurred())

			// Resolve symlinks for comparison (macOS /var -> /private/var)
			expectedPath, _ := filepath.EvalSymlinks(mainRepoDir)
			actualPath, _ := filepath.EvalSymlinks(path)
			Expect(actualPath).To(Equal(expectedPath))
		})
	})
})

func runGitCmd(repoDir string, args ...string) {
	allArgs := append([]string{"-C", repoDir}, args...)
	gitCmd := exec.Command("git", allArgs...)
	gitCmd.Stdout = GinkgoWriter
	gitCmd.Stderr = GinkgoWriter
	err := gitCmd.Run()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}
