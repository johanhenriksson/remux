package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/remux/shell"
	"github.com/spf13/cobra"
)

var herdrCmd = &cobra.Command{
	Use:   "herdr",
	Short: "herdr integration helpers",
}

var herdrSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install remux-shell so every herdr pane in a workspace gets its env",
	Long: `Creates a remux-shell symlink next to the remux binary and prints the herdr
config that makes herdr start every pane through it. remux-shell adds the env of
the remux workspace containing the pane's directory, then execs your login shell.`,
	Args: cobra.NoArgs,
	RunE: runHerdrSetup,
}

func init() {
	herdrCmd.AddCommand(herdrSetupCmd)
	rootCmd.AddCommand(herdrCmd)
}

func runHerdrSetup(cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate remux binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	link := filepath.Join(filepath.Dir(exe), shell.Name)

	switch target, err := os.Readlink(link); {
	case err == nil && target == filepath.Base(exe):
		fmt.Printf("%s already links to %s\n", link, filepath.Base(exe))
	case err == nil:
		return fmt.Errorf("%s already exists and links to %s; remove it first", link, target)
	case os.IsNotExist(err):
		if err := os.Symlink(filepath.Base(exe), link); err != nil {
			return fmt.Errorf("failed to create %s: %w", link, err)
		}
		fmt.Printf("created %s -> %s\n", link, filepath.Base(exe))
	default:
		return fmt.Errorf("%s exists and is not a symlink; remove it first", link)
	}

	fmt.Printf(`
Add this to ~/.config/herdr/config.toml, then run: herdr server reload-config

[terminal]
default_shell = %q

New panes then start through remux-shell. If the remux binary is removed,
herdr can't open panes until this line is removed again.
`, link)
	return nil
}
