package config

import (
	"path/filepath"
	"strings"
)

// promptAgents are agent CLIs that accept an initial prompt as a positional
// argument, letting them start working without typing into the TUI.
var promptAgents = map[string]bool{
	"claude": true,
	"codex":  true,
}

// AgentKind returns the agent kind ("claude", "codex") of a command line and
// its remaining arguments, or ok=false if the command isn't a known agent.
func AgentKind(cmd string) (kind string, args []string, ok bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", nil, false
	}
	kind = filepath.Base(fields[0])
	if !promptAgents[kind] {
		return "", nil, false
	}
	return kind, fields[1:], true
}

// InlinePrompt moves the tab's prompt into its command line when the command
// is an agent that accepts a positional prompt. Returns the tab unchanged
// otherwise.
func (t Tab) InlinePrompt() Tab {
	prompt := strings.TrimSpace(t.Prompt)
	if prompt == "" {
		return t
	}
	if _, _, ok := AgentKind(t.Cmd); !ok {
		return t
	}
	t.Cmd += " " + shellQuote(prompt)
	t.Prompt = ""
	return t
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
