package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var openDestDir string

var openCmd = &cobra.Command{
	Use:   "open <name>",
	Short: "Open a tmux session in the specified workspace",
	Args:  cobra.ExactArgs(1),
	RunE:  runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
	openCmd.Flags().StringVarP(&openDestDir, "dest", "d", "", "worktree directory (default: ~/at)")
}

// OpenOptions contains the parameters for opening a workspace.
type OpenOptions struct {
	DestDir       string // Worktree directory (default ~/at)
	WorkspaceName string // Name of the workspace to open
}

// OpenWorkspace opens a tmux session in the specified workspace.
// If a session with that name already exists, it attaches to it.
func OpenWorkspace(opts OpenOptions) error {
	// Build workspace path
	workspacePath := filepath.Join(opts.DestDir, opts.WorkspaceName)

	// Verify directory exists
	info, err := os.Stat(workspacePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("workspace does not exist: %s", workspacePath)
	}
	if err != nil {
		return fmt.Errorf("failed to access workspace: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace path is not a directory: %s", workspacePath)
	}

	// Verify it's a valid worktree
	if !IsWorktree(workspacePath) {
		return fmt.Errorf("not a git worktree: %s", workspacePath)
	}

	// Sanitize session name for tmux
	sessionName := SanitizeSessionName(opts.WorkspaceName)

	// Check if session already exists
	if tmuxSessionExists(sessionName) {
		return tmuxAttach(sessionName)
	}

	// Create new session and attach
	return tmuxNewSession(sessionName, workspacePath)
}

// SanitizeSessionName replaces characters that tmux doesn't allow in session names.
// Tmux disallows dots and colons in session names.
func SanitizeSessionName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

// tmuxSessionExists checks if a tmux session with the given name exists.
func tmuxSessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// tmuxAttach attaches to an existing tmux session.
func tmuxAttach(name string) error {
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// tmuxNewSession creates a new tmux session and attaches to it.
func tmuxNewSession(name, workdir string) error {
	cmd := exec.Command("tmux", "new-session", "-s", name, "-c", workdir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runOpen(cmd *cobra.Command, args []string) error {
	workspaceName := args[0]

	// Resolve destination directory
	dest := openDestDir
	if dest == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dest = filepath.Join(homeDir, "at")
	}

	// If in a git repo, prefix the repo name
	if repoRoot, err := findGitRoot(); err == nil {
		repoName := filepath.Base(repoRoot)
		workspaceName = fmt.Sprintf("%s-%s", repoName, workspaceName)
	}

	return OpenWorkspace(OpenOptions{
		DestDir:       dest,
		WorkspaceName: workspaceName,
	})
}
