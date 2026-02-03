package tmux_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/remux/tmux"
)

func TestTmux(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tmux Suite")
}

// tmuxAvailable checks if tmux is installed and accessible.
func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// waitForShellReady waits until the shell in the tmux session has initialized
// by checking for non-empty pane content (the prompt).
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

// getEnvFromShell executes echo $KEY inside the tmux session and returns the value.
// this verifies that the env var is actually accessible to the shell, not just set at the session level.
func getEnvFromShell(session, key string) (string, error) {
	marker := fmt.Sprintf("__MARKER_%d__", time.Now().UnixNano())
	cmd := fmt.Sprintf("echo '%s'$%s'%s'", marker, key, marker)

	timeout := 5 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	// Wait for shell to be ready (prompt displayed)
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

	// Final capture for error message
	out, _ := exec.Command("tmux", "capture-pane", "-t", session, "-p").Output()
	return "", fmt.Errorf("marker not found in output after %v: %s", timeout, string(out))
}

var _ = Describe("Tmux", func() {
	Describe("SessionName", func() {
		It("replaces dots with underscores", func() {
			Expect(tmux.SessionName("my.workspace")).To(Equal("my_workspace"))
		})

		It("replaces colons with underscores", func() {
			Expect(tmux.SessionName("my:workspace")).To(Equal("my_workspace"))
		})

		It("replaces multiple special characters", func() {
			Expect(tmux.SessionName("repo.name:branch")).To(Equal("repo_name_branch"))
		})

		It("leaves valid names unchanged", func() {
			Expect(tmux.SessionName("my-workspace")).To(Equal("my-workspace"))
		})
	})

	Describe("Integration", func() {
		const testSession = "automo-test-session"

		BeforeEach(func() {
			if !tmuxAvailable() {
				Skip("tmux not available")
			}
			// Ensure clean state
			tmux.KillSession(testSession)
		})

		AfterEach(func() {
			tmux.KillSession(testSession)
		})

		Describe("NewSessionDetached", func() {
			It("creates a detached session", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir, nil)
				Expect(err).NotTo(HaveOccurred())

				Expect(tmux.SessionExists(testSession)).To(BeTrue())
			})

			It("creates a session with environment variables accessible to the shell", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				env := map[string]string{
					"TEST_VAR": "test_value",
				}
				err = tmux.NewSessionDetached(testSession, workdir, env)
				Expect(err).NotTo(HaveOccurred())

				value, err := getEnvFromShell(testSession, "TEST_VAR")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal("test_value"))
			})

			It("creates a session with multiple environment variables accessible to the shell", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				env := map[string]string{
					"VAR_ONE": "value1",
					"VAR_TWO": "value2",
				}
				err = tmux.NewSessionDetached(testSession, workdir, env)
				Expect(err).NotTo(HaveOccurred())

				val1, err := getEnvFromShell(testSession, "VAR_ONE")
				Expect(err).NotTo(HaveOccurred())
				Expect(val1).To(Equal("value1"))

				val2, err := getEnvFromShell(testSession, "VAR_TWO")
				Expect(err).NotTo(HaveOccurred())
				Expect(val2).To(Equal("value2"))
			})
		})

		Describe("SessionExists", func() {
			It("returns false for non-existent session", func() {
				Expect(tmux.SessionExists("non-existent-session-12345")).To(BeFalse())
			})

			It("returns true for existing session", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir, nil)
				Expect(err).NotTo(HaveOccurred())

				Expect(tmux.SessionExists(testSession)).To(BeTrue())
			})
		})

		Describe("KillSession", func() {
			It("kills an existing session", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(tmux.SessionExists(testSession)).To(BeTrue())

				tmux.KillSession(testSession)

				Expect(tmux.SessionExists(testSession)).To(BeFalse())
			})

			It("does not error when session does not exist", func() {
				tmux.KillSession("non-existent-session-12345")
			})
		})
	})
})
