package spaces

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/johanhenriksson/automo/git"
	"github.com/johanhenriksson/automo/tmux"
)

// OpenOptions contains the parameters for opening a space.
type OpenOptions struct {
	DestDir   string            // Worktree directory
	Name      string            // Name of the space to open
	EnvVars   map[string]string // Session-level environment variables (optional)
}

// Open opens a tmux session in the specified space.
// If a session with that name already exists, it attaches to it.
func Open(opts OpenOptions) error {
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

	// Look up space port and add to env vars
	reg, err := Load(opts.DestDir)
	if err == nil {
		if space := reg.Get(opts.Name); space != nil && space.Port > 0 {
			if opts.EnvVars == nil {
				opts.EnvVars = make(map[string]string)
			}
			opts.EnvVars["SPACE_PORT"] = strconv.Itoa(space.Port)
		}
	}

	if tmux.SessionExists(opts.Name) {
		if err := setSessionEnvVars(opts.Name, opts.EnvVars); err != nil {
			return err
		}
		if tmux.InSession() {
			return tmux.SwitchTo(opts.Name)
		}
		return tmux.Attach(opts.Name)
	}

	if tmux.InSession() {
		if err := tmux.NewSessionDetached(opts.Name, spacePath); err != nil {
			return err
		}
		if err := setSessionEnvVars(opts.Name, opts.EnvVars); err != nil {
			return err
		}
		return tmux.SwitchTo(opts.Name)
	}

	if len(opts.EnvVars) > 0 {
		if err := tmux.NewSessionDetached(opts.Name, spacePath); err != nil {
			return err
		}
		if err := setSessionEnvVars(opts.Name, opts.EnvVars); err != nil {
			return err
		}
		return tmux.Attach(opts.Name)
	}

	return tmux.NewSession(opts.Name, spacePath)
}

func setSessionEnvVars(session string, envVars map[string]string) error {
	for key, value := range envVars {
		if err := tmux.SetEnvironment(session, key, value); err != nil {
			return fmt.Errorf("failed to set env var %s: %w", key, err)
		}
	}
	return nil
}
