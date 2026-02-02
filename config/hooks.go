package config

import (
	"fmt"
	"os"
	"os/exec"
)

// runHooks executes a list of hook commands in the workspace directory.
// Each command is evaluated as a template before execution.
func runHooks(commands []string, space Space, workdir string) error {
	for _, cmd := range commands {
		resolved, err := EvaluateTemplate(cmd, space)
		if err != nil {
			return fmt.Errorf("failed to evaluate hook command: %w", err)
		}

		if err := runCommand(resolved, workdir); err != nil {
			return fmt.Errorf("hook failed: %s: %w", resolved, err)
		}
	}
	return nil
}

func runCommand(command, workdir string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
