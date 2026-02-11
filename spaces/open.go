package spaces

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/tmux"
)

// OpenSessionOptions contains the parameters for opening a space session.
type OpenSessionOptions struct {
	DestDir string            // Worktree directory
	Name    string            // Name of the space to open
	EnvVars map[string]string // Session-level environment variables (optional)
}

// OpenSession opens a tmux session in the specified space.
// If a session with that name already exists, it attaches to it.
func OpenSession(opts OpenSessionOptions) error {
	spacePath := filepath.Join(opts.DestDir, opts.Name)

	info, err := os.Stat(spacePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("space does not exist: %s", spacePath)
	}
	if err != nil {
		return fmt.Errorf("failed to access space: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("space path is not a directory: %s", spacePath)
	}

	if !git.IsWorktree(spacePath) {
		return fmt.Errorf("not a git worktree: %s", spacePath)
	}

	// Load space with config
	space, err := Open(spacePath)
	if err != nil {
		return err
	}

	if opts.EnvVars == nil {
		opts.EnvVars = make(map[string]string)
	}

	// todo: maybe no longer required?
	opts.EnvVars["SPACE_PORT"] = strconv.Itoa(space.Port)

	// Merge config env vars
	resolved, err := space.ResolveEnv()
	if err != nil {
		return fmt.Errorf("failed to resolve config env vars: %w", err)
	}
	for key, value := range resolved {
		opts.EnvVars[key] = value
	}

	// Run on_open hooks
	if err := space.RunOnOpen(); err != nil {
		return err
	}

	if tmux.SessionExists(opts.Name) {
		if tmux.InSession() {
			return tmux.SwitchTo(opts.Name)
		}
		return tmux.Attach(opts.Name)
	}

	// Get configured tabs
	tabs, err := space.Tabs()
	if err != nil {
		return fmt.Errorf("failed to resolve tabs: %w", err)
	}

	// Create session detached so we can set up tabs before attaching
	if err := tmux.NewSessionDetached(opts.Name, spacePath, opts.EnvVars); err != nil {
		return err
	}

	// Set up tabs if configured
	if len(tabs) > 0 {
		if err := setupTabs(opts.Name, spacePath, tabs); err != nil {
			return fmt.Errorf("failed to setup tabs: %w", err)
		}
	}

	// Attach or switch to session
	if tmux.InSession() {
		return tmux.SwitchTo(opts.Name)
	}
	return tmux.Attach(opts.Name)
}

// setupTabs configures tmux windows based on tab configuration.
func setupTabs(session, workdir string, tabs []config.Tab) error {
	for i, tab := range tabs {
		if i == 0 {
			// First tab uses the default window (active after session creation)
			if tab.Name != "" {
				if err := tmux.RenameWindow(session, "", tab.Name); err != nil {
					return err
				}
			}
		} else {
			// Create new windows for subsequent tabs
			if err := tmux.NewWindow(session, workdir, tab.Name); err != nil {
				return err
			}
		}

		// Send command to the active window
		if tab.Cmd != "" {
			if err := tmux.SendKeys(session, "", tab.Cmd); err != nil {
				return err
			}
		}
	}

	// Select the first window
	return tmux.SelectWindow(session, "{start}")
}
