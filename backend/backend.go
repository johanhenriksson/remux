// Package backend abstracts the terminal session manager a workspace runs in.
//
// remux owns the worktree, port allocation, env templating and lifecycle hooks.
// A Backend only manages the interactive session on top of an existing
// worktree: creating it, adding tabs, focusing it and closing it.
package backend

import (
	"fmt"
	"os"
	"strings"

	"github.com/johanhenriksson/remux/config"
)

// Names of the supported backends.
const (
	Tmux  = "tmux"
	Herdr = "herdr"
)

// Default is the backend used when none is configured.
const Default = Tmux

// EnvVar names the environment variable that sets a user-level default backend.
// Config files (.remux.yaml, .remux.local.yaml) take precedence over it.
const EnvVar = "REMUX_BACKEND"

// Space identifies the workspace a backend operates on.
type Space struct {
	Name     string // remux space name (registry key)
	Path     string // worktree path
	RepoRoot string // main repository root
}

// Backend manages the terminal session of a workspace.
type Backend interface {
	// Name returns the backend name ("tmux" or "herdr").
	Name() string

	// Exists reports whether a live session exists for the space.
	Exists(space Space) (bool, error)

	// Create starts an empty, unfocused session in the space's worktree. Every
	// shell the backend starts for the session should see env.
	Create(space Space, env map[string]string) error

	// AddTabs sets up tabs in a session created by Create. The first tab
	// reuses the session's initial shell. Tabs arrive with prompts already
	// resolved and inlined where possible.
	AddTabs(space Space, tabs []config.Tab, env map[string]string) error

	// Focus attaches to or switches to the space's session.
	Focus(space Space) error

	// Close ends the session, leaving the worktree in place. It is a no-op
	// when there is no session.
	Close(space Space) error

	// Current returns which of the candidates the caller is running in.
	// inside reports whether the caller runs inside this backend at all; a
	// nil space with inside=true means the caller's session isn't one of the
	// candidates.
	Current(candidates []Space) (current *Space, inside bool, err error)
}

// New returns the backend with the given name. An empty name selects the
// user default (REMUX_BACKEND) or tmux.
func New(name string) (Backend, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(os.Getenv(EnvVar))
	}
	if name == "" {
		name = Default
	}
	switch name {
	case Tmux:
		return NewTmux(), nil
	case Herdr:
		return NewHerdr(nil), nil
	default:
		return nil, fmt.Errorf("unknown backend %q (want %q or %q)", name, Tmux, Herdr)
	}
}

// Detect returns the backend the caller is running inside, for commands that
// act on "the current session" (next, prev). tmux wins when both are present,
// since a tmux client inside a herdr pane is the innermost session.
func Detect() Backend {
	if os.Getenv("TMUX") != "" {
		return NewTmux()
	}
	if os.Getenv("HERDR_ENV") == "1" {
		return NewHerdr(nil)
	}
	b, err := New("")
	if err != nil {
		return NewTmux()
	}
	return b
}
