package tmux

import (
	"os"
	"os/exec"
	"strings"
)

// run executes a tmux command without interactive I/O.
func run(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runInteractive executes a tmux command with full I/O (for attaching).
func runInteractive(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sanitizeName replaces characters that tmux doesn't allow in session names.
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

// SessionExists checks if a tmux session with the given name exists.
func SessionExists(name string) bool {
	return run("has-session", "-t", sanitizeName(name)) == nil
}

// Attach attaches to an existing tmux session.
func Attach(name string) error {
	return runInteractive("attach-session", "-t", sanitizeName(name))
}

// NewSession creates a new tmux session and attaches to it.
func NewSession(name, workdir string, env map[string]string) error {
	args := []string{"new-session", "-s", sanitizeName(name), "-c", workdir}
	args = append(args, envArgs(env)...)
	return runInteractive(args...)
}

// NewSessionDetached creates a new tmux session without attaching.
func NewSessionDetached(name, workdir string, env map[string]string) error {
	args := []string{"new-session", "-d", "-s", sanitizeName(name), "-c", workdir}
	args = append(args, envArgs(env)...)
	return run(args...)
}

func envArgs(env map[string]string) []string {
	var args []string
	for key, value := range env {
		args = append(args, "-e", key+"="+value)
	}
	return args
}

// KillSession kills a tmux session if it exists.
func KillSession(name string) {
	run("kill-session", "-t", sanitizeName(name))
}

// SwitchTo switches to an existing tmux session (from within tmux).
func SwitchTo(name string) error {
	return run("switch-client", "-t", sanitizeName(name))
}

// InSession returns true if currently running inside a tmux session.
func InSession() bool {
	return os.Getenv("TMUX") != ""
}

// SessionName returns a sanitized session name for the given workspace name.
func SessionName(name string) string {
	return sanitizeName(name)
}

// NewWindow creates a new window in the given session.
func NewWindow(session, workdir, name string) error {
	args := []string{"new-window", "-t", sanitizeName(session), "-c", workdir}
	if name != "" {
		args = append(args, "-n", name)
	}
	return run(args...)
}

// SendKeys sends keys to a window in the given session.
// If window is empty, the active window is targeted.
func SendKeys(session, window, keys string) error {
	target := sanitizeName(session)
	if window != "" {
		target += ":" + window
	}
	return run("send-keys", "-t", target, keys, "Enter")
}

// RenameWindow renames a window in the given session.
// If target is empty, the active window is renamed.
func RenameWindow(session, target, newName string) error {
	t := sanitizeName(session)
	if target != "" {
		t += ":" + target
	}
	return run("rename-window", "-t", t, newName)
}

// SelectWindow selects a window in the given session.
// If window is empty, the active window is targeted.
func SelectWindow(session, window string) error {
	target := sanitizeName(session)
	if window != "" {
		target += ":" + window
	}
	return run("select-window", "-t", target)
}

