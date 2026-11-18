package config

import (
	"fmt"
	"strings"
)

// BaseInstructions is the built-in text given to every workspace agent,
// ahead of the project's own instructions. Disable with
// default_instructions: false.
const BaseInstructions = `You are running in a remux workspace: an isolated git worktree with its own
ports and environment, alongside other workspaces of the same repo.

- Workspace: {{ space.Name }} at {{ space.Path }}
- Main checkout: {{ space.RepoRoot }} (do not modify it)
- Ports are allocated per workspace; use the ones from this workspace's
  environment, never hardcoded defaults.

Use the remux CLI (and the remux skill, if available) for workspace operations.`

// DefaultInstructionsEnabled reports whether the built-in base instructions apply.
func (c *Config) DefaultInstructionsEnabled() bool {
	return c.DefaultInstructions == nil || *c.DefaultInstructions
}

// ResolveInstructions renders the agent instructions: the built-in base
// (unless disabled), a blank line, then the project's instructions. env is
// the space's resolved env, overlaid on the host env for {{ env.X }}.
// Returns "" when there is nothing to add.
func (c *Config) ResolveInstructions(space Space, env map[string]string) (string, error) {
	var parts []string
	if c.DefaultInstructionsEnabled() {
		base, err := EvaluateTemplateWithEnv(BaseInstructions, space, env)
		if err != nil {
			return "", fmt.Errorf("base instructions: %w", err)
		}
		parts = append(parts, base)
	}
	if strings.TrimSpace(c.Instructions) != "" {
		project, err := EvaluateTemplateWithEnv(c.Instructions, space, env)
		if err != nil {
			return "", fmt.Errorf("instructions: %w", err)
		}
		parts = append(parts, strings.TrimSpace(project))
	}
	return strings.Join(parts, "\n\n"), nil
}
