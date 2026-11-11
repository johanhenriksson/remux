package spaces

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
)

// OpenSessionOptions contains the parameters for opening a space session.
type OpenSessionOptions struct {
	DestDir string            // Worktree directory
	Name    string            // Name of the space to open
	Prompt  string            // Initial prompt for the agent, also available to tab templates
	Agent   string            // Agent to run in the first tab (overrides config, default: claude)
	EnvVars map[string]string // Extra session environment variables (optional)
	Detach  bool              // Create session without attaching
}

// OpenSession opens the space's session in its configured backend.
// If a session already exists, it focuses it. Otherwise it creates the
// session, runs on_create hooks if the space is new, runs on_open hooks, adds
// tabs and focuses the session (unless Detach is set).
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

	b, err := space.Backend()
	if err != nil {
		return err
	}
	bs := space.backendSpace()

	// Resolved config env plus SPACE_PORT, the env every session shell sees
	spaceEnv, err := space.ResolveEnv()
	if err != nil {
		return fmt.Errorf("failed to resolve config env vars: %w", err)
	}
	if spaceEnv == nil {
		spaceEnv = make(map[string]string)
	}
	// todo: maybe no longer required?
	if _, ok := spaceEnv["SPACE_PORT"]; !ok {
		spaceEnv["SPACE_PORT"] = strconv.Itoa(space.Port)
	}

	env := make(map[string]string, len(opts.EnvVars)+len(spaceEnv))
	for key, value := range opts.EnvVars {
		env[key] = value
	}
	for key, value := range spaceEnv {
		env[key] = value
	}

	exists, err := b.Exists(bs)
	if err != nil {
		return fmt.Errorf("%s: %w", b.Name(), err)
	}
	if exists {
		if err := space.RunOnOpen(); err != nil {
			return err
		}
		if opts.Detach {
			return nil
		}
		return b.Focus(bs)
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
	for i := range tabs {
		tabs[i] = inlinePrompt(tabs[i])
	}

	// Create an empty, unfocused session so we can set up tabs before focusing
	if err := b.Create(bs, env); err != nil {
		return err
	}

	// First session for a new space: run setup before tabs start
	if space.SetupPending {
		if err := setup(space, spaceEnv); err != nil {
			return err
		}
	}

	if err := space.RunOnOpen(); err != nil {
		_ = b.Close(bs)
		return err
	}

	if err := b.AddTabs(bs, tabs, env); err != nil {
		return fmt.Errorf("failed to setup tabs: %w", err)
	}

	if opts.Detach {
		return nil
	}
	return b.Focus(bs)
}

// setup writes the env file and runs on_create hooks for a newly created
// space, then clears its pending flag. Hook failures are warnings.
func setup(space *Space, env map[string]string) error {
	if path := space.EnvFile(); path != "" {
		if err := config.WriteEnvFile(path, env); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write env file: %v\n", err)
		}
	}
	space.RunOnCreate()

	destDir := filepath.Dir(space.Path)
	unlock, err := registry.Lock(destDir)
	if err != nil {
		return fmt.Errorf("failed to lock registry: %w", err)
	}
	defer unlock()
	reg, err := registry.Load(destDir)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}
	if entry := reg.Get(space.Name); entry != nil {
		entry.SetupPending = false
	}
	if err := reg.Save(destDir); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}
	space.SetupPending = false
	return nil
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
