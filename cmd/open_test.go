package cmd_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/cmd"
)

var _ = Describe("Open", func() {
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
		worktreeDir = filepath.Join(destDir, "test-workspace")
		runGitCmd(mainRepoDir, "branch", "test-branch")
		runGitCmd(mainRepoDir, "worktree", "add", worktreeDir, "test-branch")
	})

	AfterEach(func() {
		// Clean up temp directories
		os.RemoveAll(mainRepoDir)
		os.RemoveAll(destDir)
	})

	Describe("SanitizeSessionName", func() {
		It("replaces dots with underscores", func() {
			Expect(cmd.SanitizeSessionName("my.workspace")).To(Equal("my_workspace"))
		})

		It("replaces colons with underscores", func() {
			Expect(cmd.SanitizeSessionName("my:workspace")).To(Equal("my_workspace"))
		})

		It("replaces multiple special characters", func() {
			Expect(cmd.SanitizeSessionName("repo.name:branch")).To(Equal("repo_name_branch"))
		})

		It("leaves valid names unchanged", func() {
			Expect(cmd.SanitizeSessionName("my-workspace")).To(Equal("my-workspace"))
		})
	})

	Describe("OpenWorkspace", func() {
		It("returns an error for non-existent workspace", func() {
			opts := cmd.OpenOptions{
				DestDir:       destDir,
				WorkspaceName: "non-existent",
			}

			err := cmd.OpenWorkspace(opts)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("workspace does not exist"))
		})

		It("returns an error for non-worktree directory", func() {
			// Create a regular directory (not a worktree)
			regularDir := filepath.Join(destDir, "regular-dir")
			err := os.MkdirAll(regularDir, 0755)
			Expect(err).NotTo(HaveOccurred())

			opts := cmd.OpenOptions{
				DestDir:       destDir,
				WorkspaceName: "regular-dir",
			}

			err = cmd.OpenWorkspace(opts)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a git worktree"))
		})

		It("returns an error when path is a file, not a directory", func() {
			// Create a file instead of directory
			filePath := filepath.Join(destDir, "file-not-dir")
			err := os.WriteFile(filePath, []byte("test"), 0644)
			Expect(err).NotTo(HaveOccurred())

			opts := cmd.OpenOptions{
				DestDir:       destDir,
				WorkspaceName: "file-not-dir",
			}

			err = cmd.OpenWorkspace(opts)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a directory"))
		})

		// Note: We cannot easily test the actual tmux session creation in unit tests
		// as it requires a running tmux server and would create actual sessions.
		// The path validation tests above cover the validation logic.
	})
})
