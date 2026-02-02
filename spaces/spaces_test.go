package spaces_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/spaces"
	"github.com/johanhenriksson/automo/tmux"
)

func TestSpaces(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spaces Suite")
}

var _ = Describe("Registry", func() {
	var (
		reg     *spaces.Registry
		tempDir string
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "test-registry-*")
		Expect(err).NotTo(HaveOccurred())
		reg = &spaces.Registry{}
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	Describe("AllocatePort", func() {
		It("returns BasePort for empty registry", func() {
			Expect(reg.AllocatePort()).To(Equal(spaces.BasePort))
		})

		It("returns next port after single space", func() {
			reg.Add("space1", "/path/1", spaces.BasePort)
			Expect(reg.AllocatePort()).To(Equal(spaces.BasePort + spaces.PortRange))
		})

		It("returns max port + PortRange for multiple spaces", func() {
			reg.Add("space1", "/path/1", 11010)
			reg.Add("space2", "/path/2", 11020)
			reg.Add("space3", "/path/3", 11030)
			Expect(reg.AllocatePort()).To(Equal(11040))
		})

		It("handles non-sequential ports", func() {
			reg.Add("space1", "/path/1", 11010)
			reg.Add("space2", "/path/2", 11050) // gap
			Expect(reg.AllocatePort()).To(Equal(11060))
		})
	})

	Describe("Get", func() {
		It("returns nil for non-existent space", func() {
			Expect(reg.Get("missing")).To(BeNil())
		})

		It("returns pointer to existing space", func() {
			reg.Add("test", "/path/test", 11010)
			space := reg.Get("test")
			Expect(space).NotTo(BeNil())
			Expect(space.Name).To(Equal("test"))
			Expect(space.Port).To(Equal(11010))
		})
	})

	Describe("Add", func() {
		It("adds new space with port", func() {
			reg.Add("new", "/path/new", 11010)
			Expect(reg.List()).To(HaveLen(1))
			Expect(reg.List()[0].Port).To(Equal(11010))
		})

		It("updates existing space", func() {
			reg.Add("test", "/old/path", 11010)
			reg.Add("test", "/new/path", 11020)
			Expect(reg.List()).To(HaveLen(1))
			Expect(reg.List()[0].Path).To(Equal("/new/path"))
			Expect(reg.List()[0].Port).To(Equal(11020))
		})
	})

	Describe("Save and Load", func() {
		It("persists port field", func() {
			reg.Add("test", "/path/test", 11010)
			err := reg.Save(tempDir)
			Expect(err).NotTo(HaveOccurred())

			loaded, err := spaces.Load(tempDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.List()).To(HaveLen(1))
			Expect(loaded.List()[0].Port).To(Equal(11010))
		})
	})
})

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

		// Verify port allocation in registry
		reg, err := spaces.Load(destDir)
		Expect(err).NotTo(HaveOccurred())
		space := reg.Get(filepath.Base(worktreePath))
		Expect(space).NotTo(BeNil())
		Expect(space.Port).To(Equal(spaces.BasePort))
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

func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func getSessionEnv(session, key string) (string, error) {
	out, err := exec.Command("tmux", "show-environment", "-t", session, key).Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected format: %s", line)
	}
	return parts[1], nil
}

var _ = Describe("Open Integration", func() {
	var (
		mainRepoDir string
		destDir     string
		spaceName   string
	)

	BeforeEach(func() {
		if !tmuxAvailable() {
			Skip("tmux not available")
		}
		if tmux.InSession() {
			Skip("cannot run inside tmux session (would switch sessions)")
		}

		var err error
		mainRepoDir, err = os.MkdirTemp("", "test-main-repo-*")
		Expect(err).NotTo(HaveOccurred())

		destDir, err = os.MkdirTemp("", "test-dest-*")
		Expect(err).NotTo(HaveOccurred())

		// Set up git repo
		runGitCmd(mainRepoDir, "init")
		runGitCmd(mainRepoDir, "config", "user.email", "test@test.com")
		runGitCmd(mainRepoDir, "config", "user.name", "Test User")
		testFile := filepath.Join(mainRepoDir, "README.md")
		err = os.WriteFile(testFile, []byte("# Test"), 0644)
		Expect(err).NotTo(HaveOccurred())
		runGitCmd(mainRepoDir, "add", ".")
		runGitCmd(mainRepoDir, "commit", "-m", "Initial commit")
	})

	AfterEach(func() {
		if spaceName != "" {
			tmux.KillSession(spaceName)
		}
		os.RemoveAll(mainRepoDir)
		os.RemoveAll(destDir)
	})

	It("sets SPACE_PORT in tmux session environment", func() {
		// Create a space (allocates port)
		createOpts := spaces.CreateOptions{
			RepoRoot:   mainRepoDir,
			DestDir:    destDir,
			BranchName: "port-test",
		}
		worktreePath, err := spaces.Create(createOpts)
		Expect(err).NotTo(HaveOccurred())
		spaceName = filepath.Base(worktreePath)

		// Verify port was allocated
		reg, err := spaces.Load(destDir)
		Expect(err).NotTo(HaveOccurred())
		space := reg.Get(spaceName)
		Expect(space).NotTo(BeNil())
		Expect(space.Port).To(Equal(spaces.BasePort))

		// Create detached tmux session first
		err = tmux.NewSessionDetached(spaceName, worktreePath)
		Expect(err).NotTo(HaveOccurred())

		// Open the space - will set env vars, then fail on attach (not in terminal)
		openOpts := spaces.OpenOptions{
			DestDir: destDir,
			Name:    spaceName,
		}
		_ = spaces.Open(openOpts) // Ignore attach error

		// Verify SPACE_PORT was set in the tmux session
		value, err := getSessionEnv(spaceName, "SPACE_PORT")
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(strconv.Itoa(spaces.BasePort)))
	})
})
