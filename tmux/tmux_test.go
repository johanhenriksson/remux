package tmux_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/johanhenriksson/automo/tmux"
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

// getSessionEnv retrieves an environment variable from a tmux session.
func getSessionEnv(session, key string) (string, error) {
	out, err := exec.Command("tmux", "show-environment", "-t", session, key).Output()
	if err != nil {
		return "", err
	}
	// Output format: "KEY=value\n"
	line := strings.TrimSpace(string(out))
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected format: %s", line)
	}
	return parts[1], nil
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

				err = tmux.NewSessionDetached(testSession, workdir)
				Expect(err).NotTo(HaveOccurred())

				Expect(tmux.SessionExists(testSession)).To(BeTrue())
			})
		})

		Describe("SessionExists", func() {
			It("returns false for non-existent session", func() {
				Expect(tmux.SessionExists("non-existent-session-12345")).To(BeFalse())
			})

			It("returns true for existing session", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir)
				Expect(err).NotTo(HaveOccurred())

				Expect(tmux.SessionExists(testSession)).To(BeTrue())
			})
		})

		Describe("KillSession", func() {
			It("kills an existing session", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir)
				Expect(err).NotTo(HaveOccurred())
				Expect(tmux.SessionExists(testSession)).To(BeTrue())

				tmux.KillSession(testSession)

				Expect(tmux.SessionExists(testSession)).To(BeFalse())
			})

			It("does not error when session does not exist", func() {
				// KillSession returns nothing, so just verify it doesn't panic
				tmux.KillSession("non-existent-session-12345")
			})
		})

		Describe("SetEnvironment", func() {
			It("sets an environment variable on a session", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir)
				Expect(err).NotTo(HaveOccurred())

				err = tmux.SetEnvironment(testSession, "TEST_VAR", "test_value")
				Expect(err).NotTo(HaveOccurred())

				value, err := getSessionEnv(testSession, "TEST_VAR")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal("test_value"))
			})

			It("can set multiple environment variables", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir)
				Expect(err).NotTo(HaveOccurred())

				err = tmux.SetEnvironment(testSession, "VAR_ONE", "value1")
				Expect(err).NotTo(HaveOccurred())
				err = tmux.SetEnvironment(testSession, "VAR_TWO", "value2")
				Expect(err).NotTo(HaveOccurred())

				val1, err := getSessionEnv(testSession, "VAR_ONE")
				Expect(err).NotTo(HaveOccurred())
				Expect(val1).To(Equal("value1"))

				val2, err := getSessionEnv(testSession, "VAR_TWO")
				Expect(err).NotTo(HaveOccurred())
				Expect(val2).To(Equal("value2"))
			})

			It("can overwrite an existing environment variable", func() {
				workdir, err := os.Getwd()
				Expect(err).NotTo(HaveOccurred())

				err = tmux.NewSessionDetached(testSession, workdir)
				Expect(err).NotTo(HaveOccurred())

				err = tmux.SetEnvironment(testSession, "TEST_VAR", "original")
				Expect(err).NotTo(HaveOccurred())

				err = tmux.SetEnvironment(testSession, "TEST_VAR", "updated")
				Expect(err).NotTo(HaveOccurred())

				value, err := getSessionEnv(testSession, "TEST_VAR")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal("updated"))
			})
		})
	})
})
