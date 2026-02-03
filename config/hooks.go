package config

import (
	"fmt"
	"os"
	"os/exec"
)

// runHooks executes a list of hook commands in the workspace directory.
// Each command is evaluated as a template before execution.
func runHooks(commands []string, space Space, workdir string, env map[string]string) error {
	for _, cmd := range commands {
		resolved, err := EvaluateTemplate(cmd, space)
		if err != nil {
			return fmt.Errorf("failed to evaluate hook command: %w", err)
		}

		if err := runCommand(resolved, workdir, env); err != nil {
			return fmt.Errorf("hook failed: %s: %w", resolved, err)
		}
	}
	return nil
}

func runCommand(command, workdir string, env map[string]string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Combine parent environment with custom env vars
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	return cmd.Run()
}
