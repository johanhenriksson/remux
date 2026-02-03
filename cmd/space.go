package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
	"github.com/spf13/cobra"
)

var destDir string

var newCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a new workspace",
	Args:  cobra.ExactArgs(1),
	RunE:  runNew,
}

var openCmd = &cobra.Command{
	Use:   "open <name>",
	Short: "Open or resume a workspace session",
	Args:  cobra.ExactArgs(1),
	RunE:  runOpen,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tracked workspaces",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(listCmd)

	newCmd.Flags().StringVarP(&destDir, "dest", "d", "", "destination directory for worktrees (default: ~/.remux)")
	openCmd.Flags().StringVarP(&destDir, "dest", "d", "", "worktree directory (default: ~/.remux)")
}

func getDestDir() (string, error) {
	return resolveDestDir(destDir)
}

// resolveDestDir resolves the destination directory, expanding ~ and making it absolute.
func resolveDestDir(dest string) (string, error) {
	if dest == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return filepath.Join(homeDir, ".remux"), nil
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

func runNew(cmd *cobra.Command, args []string) error {
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

func runOpen(cmd *cobra.Command, args []string) error {
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

func runList(cmd *cobra.Command, args []string) error {
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
