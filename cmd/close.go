package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
	"github.com/johanhenriksson/remux/tmux"
	"github.com/spf13/cobra"
)

var closeCmd = &cobra.Command{
	Use:   "close [name]",
	Short: "Close the tmux session for a workspace without removing it",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runClose,
}

func init() {
	closeCmd.Flags().StringVar(&pathDir, "path", "", "worktree directory (default: ~/.remux)")
	rootCmd.AddCommand(closeCmd)
}

func runClose(cmd *cobra.Command, args []string) error {
	var spaceName string

	if len(args) == 1 {
		dest, err := getPath()
		if err != nil {
			return err
		}
		reg, err := registry.Load(dest)
		if err != nil {
			return fmt.Errorf("failed to load registry: %w", err)
		}
		entry := reg.Get(args[0])
		if entry == nil {
			entry = reg.GetByBranch(args[0])
		}
		if entry == nil {
			return fmt.Errorf("space not found: %s", args[0])
		}
		spaceName = entry.Name
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		root, err := spaces.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not in a remux workspace: %w", err)
		}
		spaceName = filepath.Base(root)
	}

	sessionName := tmux.SessionName(spaceName)
	if !tmux.SessionExists(sessionName) {
		return fmt.Errorf("no active session for %s", spaceName)
	}

	tmux.KillSession(spaceName)
	fmt.Printf("Closed session: %s\n", spaceName)
	return nil
}
