package spaces

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/tmux"
)

// OpenSessionOptions contains the parameters for opening a space session.
type OpenSessionOptions struct {
	DestDir string            // Worktree directory
	Name    string            // Name of the space to open
	Prompt  string            // Initial prompt for the agent, also available to tab templates
	Agent   string            // Agent to run in the first tab (overrides config, default: claude)
	EnvVars map[string]string // Session-level environment variables (optional)
	Detach  bool              // Create session without attaching
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
		if opts.Detach {
			return nil
		}
		if tmux.InSession() {
			return tmux.SwitchTo(opts.Name)
		}
		return tmux.Attach(opts.Name)
	}

	// Get configured tabs
	tabs, err := space.Tabs(opts.Prompt)
	if err != nil {
		return fmt.Errorf("failed to resolve tabs: %w", err)
	}

	// The agent runs in the first tab; configured tabs come after it
	agent := opts.Agent
	if agent == "" {
		agent = space.Agent()
	}
	if tab, ok := agentTab(agent, opts.Prompt); ok {
		tabs = append([]config.Tab{tab}, tabs...)
	}

	// Create session detached so we can set up tabs before attaching
	if err := tmux.NewSessionDetached(opts.Name, spacePath, opts.EnvVars); err != nil {
		return err
	}

	if err := setupTabs(opts.Name, spacePath, tabs); err != nil {
		return fmt.Errorf("failed to setup tabs: %w", err)
	}

	if opts.Detach {
		return nil
	}

	// Attach or switch to session
	if tmux.InSession() {
		return tmux.SwitchTo(opts.Name)
	}
	return tmux.Attach(opts.Name)
}

// agentCommands maps known agent names to the command that starts them. These
// CLIs accept an initial prompt as a positional argument, letting them start
// working without typing into the TUI.
var agentCommands = map[string]string{
	"claude": "claude --permission-mode auto",
	"codex":  "codex",
}

const defaultAgent = "claude"

// agentTab builds the leading tab that runs the agent. Unknown agents are used
// as the command line verbatim, so arbitrary commands and flags work too.
// Returns false for the agent "none", which disables the agent tab entirely.
func agentTab(agent, prompt string) (config.Tab, bool) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = defaultAgent
	}
	if agent == "none" {
		return config.Tab{}, false
	}
	cmd, known := agentCommands[agent]
	if !known {
		cmd = agent
	}
	return config.Tab{
		Name:   filepath.Base(strings.Fields(cmd)[0]),
		Cmd:    cmd,
		Prompt: prompt,
	}, true
}

// inlinePrompt moves a tab's prompt into its command line when the command is
// an agent that accepts a positional prompt. Returns the tab unchanged
// otherwise.
func inlinePrompt(tab config.Tab) config.Tab {
	prompt := strings.TrimSpace(tab.Prompt)
	fields := strings.Fields(tab.Cmd)
	if prompt == "" || len(fields) == 0 {
		return tab
	}
	if _, known := agentCommands[filepath.Base(fields[0])]; !known {
		return tab
	}
	tab.Cmd += " " + shellQuote(prompt)
	tab.Prompt = ""
	return tab
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// setupTabs configures tmux windows based on tab configuration.
func setupTabs(session, workdir string, tabs []config.Tab) error {
	for i, tab := range tabs {
		tab = inlinePrompt(tab)

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

		// Handle prompt/await
		prompt := strings.TrimSpace(tab.Prompt)
		if prompt != "" {
			if tab.Await != "" {
				timeout := time.Duration(tab.Timeout) * time.Second
				if timeout == 0 {
					timeout = 60 * time.Second
				}
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
