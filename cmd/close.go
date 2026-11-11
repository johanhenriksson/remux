package cmd

import (
	"fmt"
	"os"

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
	"github.com/spf13/cobra"
)

var closeCmd = &cobra.Command{
	Use:   "close [name]",
	Short: "Close the session for a workspace without removing it",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runClose,
}

func init() {
	closeCmd.Flags().StringVar(&pathDir, "path", "", "worktree directory (default: ~/.remux)")
	rootCmd.AddCommand(closeCmd)
}

func runClose(cmd *cobra.Command, args []string) error {
	var spacePath string

	if len(args) == 1 {
		dest, err := getPath()
		if err != nil {
			return err
		}
		reg, err := registry.Load(dest)
		if err != nil {
			return fmt.Errorf("failed to load registry: %w", err)
		}
		entry := findEntry(reg, args[0])
		if entry == nil {
			return fmt.Errorf("space not found: %s", args[0])
		}
		spacePath = entry.Path
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		root, err := spaces.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not in a remux workspace: %w", err)
		}
		spacePath = root
	}

	space, err := spaces.Open(spacePath)
	if err != nil {
		return err
	}

	active, err := space.SessionActive()
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("no active session for %s", space.Name)
	}

	if err := space.CloseSession(); err != nil {
		return err
	}
	fmt.Printf("Closed session: %s\n", space.Name)
	return nil
}
