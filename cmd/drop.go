package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/remux/spaces"
	"github.com/spf13/cobra"
)

var dropCmd = &cobra.Command{
	Use:   "drop",
	Short: "Remove the current workspace and clean up",
	Args:  cobra.NoArgs,
	RunE:  runDrop,
}

func init() {
	rootCmd.AddCommand(dropCmd)
}

func runDrop(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	if err := spaces.Drop(cwd); err != nil {
		return err
	}

	fmt.Printf("Removed space: %s\n", filepath.Base(cwd))
	return nil
}
