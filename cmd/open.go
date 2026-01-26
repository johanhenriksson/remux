package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/automo/git"
	"github.com/johanhenriksson/automo/tmux"
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
	if !git.IsWorktree(workspacePath) {
		return fmt.Errorf("not a git worktree: %s", workspacePath)
	}

	// Check if session already exists
	if tmux.SessionExists(opts.WorkspaceName) {
		if tmux.InSession() {
			return tmux.SwitchTo(opts.WorkspaceName)
		}
		return tmux.Attach(opts.WorkspaceName)
	}

	// If already in tmux, create detached session and switch to it
	if tmux.InSession() {
		if err := tmux.NewSessionDetached(opts.WorkspaceName, workspacePath); err != nil {
			return err
		}
		return tmux.SwitchTo(opts.WorkspaceName)
	}

	// Create new session and attach
	return tmux.NewSession(opts.WorkspaceName, workspacePath)
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
	if repoRoot, err := git.FindRoot(); err == nil {
		repoName := filepath.Base(repoRoot)
		workspaceName = fmt.Sprintf("%s-%s", repoName, workspaceName)
	}

	return OpenWorkspace(OpenOptions{
		DestDir:       dest,
		WorkspaceName: workspaceName,
	})
}
