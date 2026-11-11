package backend

import (
	"fmt"
	"strings"
	"time"

	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/tmux"
)

// tmuxBackend runs each workspace as a tmux session named after the space.
type tmuxBackend struct{}

// NewTmux returns the tmux backend.
func NewTmux() Backend { return tmuxBackend{} }

func (tmuxBackend) Name() string { return Tmux }

func (tmuxBackend) Exists(space Space) (bool, error) {
	return tmux.SessionExists(space.Name), nil
}

// Create starts a detached session. tmux session env reaches every window,
// including ones opened by hand later.
func (tmuxBackend) Create(space Space, env map[string]string) error {
	return tmux.NewSessionDetached(space.Name, space.Path, env)
}

func (tmuxBackend) AddTabs(space Space, tabs []config.Tab, env map[string]string) error {
	session := space.Name
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
			if err := tmux.NewWindow(session, space.Path, tab.Name); err != nil {
				return err
			}
		}

		// Send command to the active window
		if tab.Cmd != "" {
			if err := tmux.SendKeys(session, "", tab.Cmd); err != nil {
				return err
			}
		}

		// Handle prompt/await
		prompt := strings.TrimSpace(tab.Prompt)
		if prompt != "" {
			if tab.Await != "" {
				timeout := awaitTimeout(tab)
				fmt.Printf("waiting for %q in tab %q...\n", tab.Await, tab.Name)
				if err := tmux.AwaitText(session, "", tab.Await, timeout); err != nil {
					return fmt.Errorf("tab %q: %w", tab.Name, err)
				}
			}
			// Give TUI time to become interactive after awaited text appears
			time.Sleep(1 * time.Second)
			if err := tmux.SendText(session, "", prompt); err != nil {
				return err
			}
		}
	}

	// Select the first window
	return tmux.SelectWindow(session, "{start}")
}

func (tmuxBackend) Focus(space Space) error {
	if tmux.InSession() {
		return tmux.SwitchTo(space.Name)
	}
	return tmux.Attach(space.Name)
}

func (tmuxBackend) Close(space Space) error {
	tmux.KillSession(space.Name)
	return nil
}

func (tmuxBackend) Current(candidates []Space) (*Space, bool, error) {
	if !tmux.InSession() {
		return nil, false, nil
	}
	current, err := tmux.CurrentSession()
	if err != nil {
		return nil, true, fmt.Errorf("failed to get current session: %w", err)
	}
	for i := range candidates {
		if tmux.SessionName(candidates[i].Name) == current {
			return &candidates[i], true, nil
		}
	}
	return nil, true, nil
}

// awaitTimeout returns the tab's await timeout, defaulting to 60 seconds.
func awaitTimeout(tab config.Tab) time.Duration {
	timeout := time.Duration(tab.Timeout) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return timeout
}
