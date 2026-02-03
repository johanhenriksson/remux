package spaces_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
	"github.com/johanhenriksson/remux/tmux"
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

		// Verify port allocation in registry
		reg, err := registry.Load(destDir)
		Expect(err).NotTo(HaveOccurred())
		entry := reg.Get(filepath.Base(worktreePath))
		Expect(entry).NotTo(BeNil())
		Expect(entry.Port).To(Equal(registry.BasePort))
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
		opts := spaces.OpenSessionOptions{
			DestDir: destDir,
			Name:    "non-existent",
		}

		err := spaces.OpenSession(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("space does not exist"))
	})

	It("returns an error for non-worktree directory", func() {
		regularDir := filepath.Join(destDir, "regular-dir")
		err := os.MkdirAll(regularDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		opts := spaces.OpenSessionOptions{
			DestDir: destDir,
			Name:    "regular-dir",
		}

		err = spaces.OpenSession(opts)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a git worktree"))
	})

	It("returns an error when path is a file, not a directory", func() {
		filePath := filepath.Join(destDir, "file-not-dir")
		err := os.WriteFile(filePath, []byte("test"), 0644)
		Expect(err).NotTo(HaveOccurred())

		opts := spaces.OpenSessionOptions{
			DestDir: destDir,
			Name:    "file-not-dir",
		}

		err = spaces.OpenSession(opts)

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

// waitForShellReady waits until the shell in the tmux session has initialized.
func waitForShellReady(session string, timeout time.Duration) error {
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "capture-pane", "-t", session, "-p").Output()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("shell not ready after %v", timeout)
}

func getEnvFromShell(session, key string) (string, error) {
	marker := fmt.Sprintf("__MARKER_%d__", time.Now().UnixNano())
	cmd := fmt.Sprintf("echo '%s'$%s'%s'", marker, key, marker)

	timeout := 5 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	// Wait for shell to be ready
	if err := waitForShellReady(session, timeout); err != nil {
		return "", err
	}

	// Send the command
	if err := exec.Command("tmux", "send-keys", "-t", session, cmd, "Enter").Run(); err != nil {
		return "", fmt.Errorf("send-keys failed: %w", err)
	}

	// Poll for the marker in the output
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "capture-pane", "-t", session, "-p").Output()
		if err != nil {
			time.Sleep(interval)
			continue
		}

		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, marker) && strings.HasSuffix(line, marker) {
				value := strings.TrimPrefix(line, marker)
				value = strings.TrimSuffix(value, marker)
				return value, nil
			}
		}

		time.Sleep(interval)
	}

	out, _ := exec.Command("tmux", "capture-pane", "-t", session, "-p").Output()
	return "", fmt.Errorf("marker not found in output after %v: %s", timeout, string(out))
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
		reg, err := registry.Load(destDir)
		Expect(err).NotTo(HaveOccurred())
		entry := reg.Get(spaceName)
		Expect(entry).NotTo(BeNil())
		Expect(entry.Port).To(Equal(registry.BasePort))

		// Open the space - creates session with env vars, then fails on attach (not in terminal)
		openOpts := spaces.OpenSessionOptions{
			DestDir: destDir,
			Name:    spaceName,
		}
		_ = spaces.OpenSession(openOpts) // Ignore attach error

		// Verify SPACE_PORT is accessible in the shell
		value, err := getEnvFromShell(spaceName, "SPACE_PORT")
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(strconv.Itoa(registry.BasePort)))
	})
})
