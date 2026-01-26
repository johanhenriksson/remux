package cmd_test

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/cmd"
)

var _ = Describe("Drop", func() {
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

	Describe("DropWorktree", func() {
		It("removes a worktree successfully", func() {
			err := cmd.DropWorktree(worktreeDir)

			Expect(err).NotTo(HaveOccurred())

			// Verify worktree directory was removed
			_, err = os.Stat(worktreeDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			// Verify branch still exists
			gitCmd := exec.Command("git", "-C", mainRepoDir, "show-ref", "--verify", "refs/heads/test-branch")
			err = gitCmd.Run()
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns an error when not in a worktree", func() {
			err := cmd.DropWorktree(mainRepoDir)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not in a git worktree"))
		})

		It("returns an error when there are uncommitted changes", func() {
			testFile := filepath.Join(worktreeDir, "uncommitted.txt")
			err := os.WriteFile(testFile, []byte("uncommitted"), 0644)
			Expect(err).NotTo(HaveOccurred())

			err = cmd.DropWorktree(worktreeDir)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("uncommitted changes"))

			// Verify worktree still exists
			_, err = os.Stat(worktreeDir)
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns an error for a non-git directory", func() {
			nonGitDir, err := os.MkdirTemp("", "non-git-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(nonGitDir)

			err = cmd.DropWorktree(nonGitDir)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not in a git worktree"))
		})
	})
})
