package spaces_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/spaces"
)

func TestSpaces(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spaces Suite")
}

var _ = Describe("Create", func() {
	var (
		testRepoDir string
		destDir     string
	)

	BeforeEach(func() {
		var err error

		testRepoDir, err = os.MkdirTemp("", "test-repo-*")
		Expect(err).NotTo(HaveOccurred())

		destDir, err = os.MkdirTemp("", "test-dest-*")
		Expect(err).NotTo(HaveOccurred())

		runGitCmd(testRepoDir, "init")
		runGitCmd(testRepoDir, "config", "user.email", "test@test.com")
		runGitCmd(testRepoDir, "config", "user.name", "Test User")

		testFile := filepath.Join(testRepoDir, "README.md")
		err = os.WriteFile(testFile, []byte("# Test"), 0644)
		Expect(err).NotTo(HaveOccurred())
		runGitCmd(testRepoDir, "add", ".")
		runGitCmd(testRepoDir, "commit", "-m", "Initial commit")
	})

	AfterEach(func() {
		os.RemoveAll(testRepoDir)
		os.RemoveAll(destDir)
	})

	It("creates a branch and worktree successfully", func() {
		opts := spaces.CreateOptions{
			RepoRoot:   testRepoDir,
			DestDir:    destDir,
			BranchName: "feature-test",
		}

		worktreePath, err := spaces.Create(opts)

		Expect(err).NotTo(HaveOccurred())
		expectedPath := filepath.Join(destDir, filepath.Base(testRepoDir)+"-feature-test")
		Expect(worktreePath).To(Equal(expectedPath))

		_, err = os.Stat(worktreePath)
		Expect(err).NotTo(HaveOccurred())

		gitCmd := exec.Command("git", "-C", testRepoDir, "show-ref", "--verify", "refs/heads/feature-test")
		err = gitCmd.Run()
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns an error when branch already exists", func() {
		runGitCmd(testRepoDir, "branch", "existing-branch")

		opts := spaces.CreateOptions{
			RepoRoot:   testRepoDir,
			DestDir:    destDir,
			BranchName: "existing-branch",
		}

		_, err := spaces.Create(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("already exists"))
	})

	It("returns an error when worktree directory already exists", func() {
		worktreePath := filepath.Join(destDir, filepath.Base(testRepoDir)+"-blocked-branch")
		err := os.MkdirAll(worktreePath, 0755)
		Expect(err).NotTo(HaveOccurred())

		opts := spaces.CreateOptions{
			RepoRoot:   testRepoDir,
			DestDir:    destDir,
			BranchName: "blocked-branch",
		}

		_, err = spaces.Create(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("worktree directory already exists"))
	})

	It("returns an error when not in a git repository", func() {
		nonGitDir, err := os.MkdirTemp("", "non-git-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(nonGitDir)

		opts := spaces.CreateOptions{
			RepoRoot:   nonGitDir,
			DestDir:    destDir,
			BranchName: "test-branch",
		}

		_, err = spaces.Create(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create branch"))
	})
})

var _ = Describe("Open", func() {
	var (
		mainRepoDir string
		worktreeDir string
		destDir     string
	)

	BeforeEach(func() {
		var err error

		mainRepoDir, err = os.MkdirTemp("", "test-main-repo-*")
		Expect(err).NotTo(HaveOccurred())

		destDir, err = os.MkdirTemp("", "test-dest-*")
		Expect(err).NotTo(HaveOccurred())

		runGitCmd(mainRepoDir, "init")
		runGitCmd(mainRepoDir, "config", "user.email", "test@test.com")
		runGitCmd(mainRepoDir, "config", "user.name", "Test User")

		testFile := filepath.Join(mainRepoDir, "README.md")
		err = os.WriteFile(testFile, []byte("# Test"), 0644)
		Expect(err).NotTo(HaveOccurred())
		runGitCmd(mainRepoDir, "add", ".")
		runGitCmd(mainRepoDir, "commit", "-m", "Initial commit")

		worktreeDir = filepath.Join(destDir, "test-workspace")
		runGitCmd(mainRepoDir, "branch", "test-branch")
		runGitCmd(mainRepoDir, "worktree", "add", worktreeDir, "test-branch")
	})

	AfterEach(func() {
		os.RemoveAll(mainRepoDir)
		os.RemoveAll(destDir)
	})

	It("returns an error for non-existent space", func() {
		opts := spaces.OpenOptions{
			DestDir: destDir,
			Name:    "non-existent",
		}

		err := spaces.Open(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("space does not exist"))
	})

	It("returns an error for non-worktree directory", func() {
		regularDir := filepath.Join(destDir, "regular-dir")
		err := os.MkdirAll(regularDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		opts := spaces.OpenOptions{
			DestDir: destDir,
			Name:    "regular-dir",
		}

		err = spaces.Open(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a git worktree"))
	})

	It("returns an error when path is a file, not a directory", func() {
		filePath := filepath.Join(destDir, "file-not-dir")
		err := os.WriteFile(filePath, []byte("test"), 0644)
		Expect(err).NotTo(HaveOccurred())

		opts := spaces.OpenOptions{
			DestDir: destDir,
			Name:    "file-not-dir",
		}

		err = spaces.Open(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a directory"))
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
