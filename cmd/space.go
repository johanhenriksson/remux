package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/automo/git"
	"github.com/johanhenriksson/automo/registry"
	"github.com/johanhenriksson/automo/spaces"
	"github.com/spf13/cobra"
)

var spaceDestDir string

var spaceCmd = &cobra.Command{
	Use:   "space",
	Short: "Manage workspaces",
}

var spaceNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a new branch and worktree",
	Args:  cobra.ExactArgs(1),
	RunE:  runSpaceNew,
}

var spaceOpenCmd = &cobra.Command{
	Use:   "open <name>",
	Short: "Open a tmux session in the specified workspace",
	Args:  cobra.ExactArgs(1),
	RunE:  runSpaceOpen,
}

var spaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tracked workspaces",
	Args:  cobra.NoArgs,
	RunE:  runSpaceList,
}

func init() {
	rootCmd.AddCommand(spaceCmd)
	spaceCmd.AddCommand(spaceNewCmd)
	spaceCmd.AddCommand(spaceOpenCmd)
	spaceCmd.AddCommand(spaceListCmd)

	spaceNewCmd.Flags().StringVarP(&spaceDestDir, "dest", "d", "", "destination directory for worktrees (default: ~/at)")
	spaceOpenCmd.Flags().StringVarP(&spaceDestDir, "dest", "d", "", "worktree directory (default: ~/at)")
}

func getDestDir() (string, error) {
	return resolveDestDir(spaceDestDir)
}

// resolveDestDir resolves the destination directory, expanding ~ and making it absolute.
func resolveDestDir(dest string) (string, error) {
	if dest == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return filepath.Join(homeDir, "at"), nil
	}

	// Expand ~ to home directory
	if len(dest) > 1 && dest[:2] == "~/" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		dest = filepath.Join(homeDir, dest[2:])
	}

	// Make absolute
	return filepath.Abs(dest)
}

func runSpaceNew(cmd *cobra.Command, args []string) error {
	branchName := args[0]

	repoRoot, err := git.FindRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	if git.IsWorktree(repoRoot) {
		repoRoot, err = git.GetMainRepoPath(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to find main repository: %w", err)
		}
	}

	dest, err := getDestDir()
	if err != nil {
		return err
	}

	worktreePath, err := spaces.Create(spaces.CreateOptions{
		RepoRoot:   repoRoot,
		DestDir:    dest,
		BranchName: branchName,
	})
	if err != nil {
		return err
	}

	return spaces.OpenSession(spaces.OpenSessionOptions{
		DestDir: dest,
		Name:    filepath.Base(worktreePath),
	})
}

func runSpaceOpen(cmd *cobra.Command, args []string) error {
	spaceName := args[0]

	dest, err := getDestDir()
	if err != nil {
		return err
	}

	// If in a git repo, prefix the repo name
	if repoRoot, err := git.FindRoot(); err == nil {
		repoName := filepath.Base(repoRoot)
		spaceName = fmt.Sprintf("%s-%s", repoName, spaceName)
	}

	return spaces.OpenSession(spaces.OpenSessionOptions{
		DestDir: dest,
		Name:    spaceName,
	})
}

func runSpaceList(cmd *cobra.Command, args []string) error {
	dest, err := getDestDir()
	if err != nil {
		return err
	}

	reg, err := registry.Load(dest)
	if err != nil {
		return fmt.Errorf("failed to load space registry: %w", err)
	}

	entries := reg.List()
	if len(entries) == 0 {
		fmt.Println("No tracked spaces")
		return nil
	}

	for _, e := range entries {
		fmt.Printf("%s\t%s\n", e.Name, e.Path)
	}
	return nil
}
