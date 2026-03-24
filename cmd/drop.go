package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
	"github.com/spf13/cobra"
)

var forceFlag bool

var dropCmd = &cobra.Command{
	Use:   "drop [name]",
	Short: "Remove a workspace and clean up",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDrop,
}

func init() {
	dropCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "force drop even with uncommitted changes")
	dropCmd.Flags().StringVar(&pathDir, "path", "", "worktree directory (default: ~/.remux)")
	rootCmd.AddCommand(dropCmd)
}

func runDrop(cmd *cobra.Command, args []string) error {
	var spacePath string
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
		spacePath = entry.Path
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
		spacePath = root
		spaceName = filepath.Base(root)
	}

	if err := spaces.Drop(spacePath, forceFlag); err != nil {
		return err
	}

	fmt.Printf("Removed space: %s\n", spaceName)

	// Offer to attach to another session if any remain
	dest, err := getPath()
	if err != nil {
		return nil
	}
	targets, err := activeRemuxSessions(dest)
	if err != nil || len(targets) == 0 {
		return nil
	}

	target := targets[len(targets)-1]
	fmt.Printf("Attach to %s? [Enter/Esc] ", target)

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	if line == "\n" {
		return navigate(-1)
	}

	return nil
}
