package spaces

import (
	"fmt"
	"os"
	"strings"

	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/git"
)

// instructionsFile is the name of the rendered instructions file inside the
// worktree's git dir, where it stays out of `git status`.
const instructionsFile = "remux-instructions.md"

// Instructions renders the workspace instructions for the agent. env is the
// session env, visible to the template as {{ env.X }}.
func (s *Space) Instructions(env map[string]string) (string, error) {
	return s.config.ResolveInstructions(s.configSpace(), env)
}

// applyInstructions adds the workspace instructions to the agent tab's
// arguments, in the agent's system prompt (claude) or developer instructions
// (codex). Other agent commands get nothing; a warning is printed only when
// the project set instructions explicitly.
func applyInstructions(tab *config.Tab, space *Space, env map[string]string) error {
	kind, _, known := config.AgentKind(tab.Cmd)
	if !known {
		if strings.TrimSpace(space.config.Instructions) != "" {
			fmt.Fprintf(os.Stderr, "warning: agent %q does not support workspace instructions; skipping them\n", tab.Cmd)
		}
		return nil
	}

	text, err := space.Instructions(env)
	if err != nil {
		return fmt.Errorf("failed to resolve instructions: %w", err)
	}
	if text == "" {
		return nil
	}

	switch kind {
	case "claude":
		// A file keeps the pane's command line short and avoids quoting
		// multi-line text through the backends
		path, err := git.GitPath(space.Path, instructionsFile)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(text+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to write instructions: %w", err)
		}
		tab.AgentArgs = append(tab.AgentArgs, "--append-system-prompt-file", path)
	case "codex":
		// codex has no file option for extra instructions. Escaping the
		// newlines keeps the value on one line, which herdr requires.
		tab.AgentArgs = append(tab.AgentArgs, "-c", "developer_instructions="+tomlString(text))
	}
	return nil
}

// tomlString encodes s as a single-line TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
