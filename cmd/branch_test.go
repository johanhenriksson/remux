package cmd_test

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/cmd"
)

var _ = Describe("CreateBranch", func() {
	var (
		testRepoDir string
		destDir     string
	)

	BeforeEach(func() {
		var err error

		// Create temp directory for test git repo
		testRepoDir, err = os.MkdirTemp("", "test-repo-*")
		Expect(err).NotTo(HaveOccurred())

		// Create temp directory for worktree destination
		destDir, err = os.MkdirTemp("", "test-dest-*")
		Expect(err).NotTo(HaveOccurred())

		// Initialize git repo
		runGitCmd(testRepoDir, "init")
		runGitCmd(testRepoDir, "config", "user.email", "test@test.com")
		runGitCmd(testRepoDir, "config", "user.name", "Test User")

		// Create initial commit
		testFile := filepath.Join(testRepoDir, "README.md")
		err = os.WriteFile(testFile, []byte("# Test"), 0644)
		Expect(err).NotTo(HaveOccurred())
		runGitCmd(testRepoDir, "add", ".")
		runGitCmd(testRepoDir, "commit", "-m", "Initial commit")
	})

	AfterEach(func() {
		// Clean up temp directories
		os.RemoveAll(testRepoDir)
		os.RemoveAll(destDir)
	})

	It("creates a branch and worktree successfully", func() {
		opts := cmd.BranchOptions{
			RepoRoot:   testRepoDir,
			DestDir:    destDir,
			BranchName: "feature-test",
		}

		worktreePath, err := cmd.CreateBranch(opts)

		Expect(err).NotTo(HaveOccurred())
		expectedPath := filepath.Join(destDir, filepath.Base(testRepoDir)+"-feature-test")
		Expect(worktreePath).To(Equal(expectedPath))

		// Verify worktree was created
		_, err = os.Stat(worktreePath)
		Expect(err).NotTo(HaveOccurred())

		// Verify branch exists
		gitCmd := exec.Command("git", "-C", testRepoDir, "show-ref", "--verify", "refs/heads/feature-test")
		err = gitCmd.Run()
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns an error when branch already exists", func() {
		// Create the branch first
		runGitCmd(testRepoDir, "branch", "existing-branch")

		opts := cmd.BranchOptions{
			RepoRoot:   testRepoDir,
			DestDir:    destDir,
			BranchName: "existing-branch",
		}

		_, err := cmd.CreateBranch(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("already exists"))
	})

	It("returns an error when worktree directory already exists", func() {
		// Create the worktree directory first
		worktreePath := filepath.Join(destDir, filepath.Base(testRepoDir)+"-blocked-branch")
		err := os.MkdirAll(worktreePath, 0755)
		Expect(err).NotTo(HaveOccurred())

		opts := cmd.BranchOptions{
			RepoRoot:   testRepoDir,
			DestDir:    destDir,
			BranchName: "blocked-branch",
		}

		_, err = cmd.CreateBranch(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("worktree directory already exists"))
	})

	It("returns an error when not in a git repository", func() {
		// Create a non-git directory
		nonGitDir, err := os.MkdirTemp("", "non-git-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(nonGitDir)

		opts := cmd.BranchOptions{
			RepoRoot:   nonGitDir,
			DestDir:    destDir,
			BranchName: "test-branch",
		}

		_, err = cmd.CreateBranch(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create branch"))
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
