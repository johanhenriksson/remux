package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
	"github.com/johanhenriksson/remux/tmux"
	"github.com/spf13/cobra"
)

var pathDir string
var detachFlag bool
var promptFlag string
var agentFlag string

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

	newCmd.Flags().StringVar(&pathDir, "path", "", "destination directory for worktrees (default: ~/.remux)")
	newCmd.Flags().BoolVarP(&detachFlag, "detach", "d", false, "create workspace without attaching to the session")
	newCmd.Flags().StringVarP(&promptFlag, "prompt", "p", "", "initial prompt for the agent")
	newCmd.Flags().StringVar(&agentFlag, "agent", "", "agent to run in the first tab (default: claude)")
	openCmd.Flags().StringVar(&pathDir, "path", "", "worktree directory (default: ~/.remux)")
	openCmd.Flags().StringVarP(&promptFlag, "prompt", "p", "", "initial prompt for the agent")
	openCmd.Flags().StringVar(&agentFlag, "agent", "", "agent to run in the first tab (default: claude)")
}

func getPath() (string, error) {
	return resolveDestDir(pathDir)
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

func confirmPrompt(message string) bool {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

func runNew(cmd *cobra.Command, args []string) error {
	branchName := args[0]

	repoRoot, err := git.FindRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	dest, err := getPath()
	if err != nil {
		return err
	}

	reuseExisting := false
	if git.BranchExists(repoRoot, branchName) {
		if !confirmPrompt(fmt.Sprintf("Branch %q already exists. Reuse it? [y/N] ", branchName)) {
			return nil
		}
		reuseExisting = true
	}

	worktreePath, err := spaces.Create(spaces.CreateOptions{
		RepoRoot:            repoRoot,
		DestDir:             dest,
		BranchName:          branchName,
		ReuseExistingBranch: reuseExisting,
	})
	if err != nil {
		return err
	}

	return spaces.OpenSession(spaces.OpenSessionOptions{
		DestDir: dest,
		Name:    filepath.Base(worktreePath),
		Prompt:  promptFlag,
		Agent:   agentFlag,
		Detach:  detachFlag,
	})
}

func runOpen(cmd *cobra.Command, args []string) error {
	spaceName := args[0]

	dest, err := getPath()
	if err != nil {
		return err
	}

	// If in a git repo, prefix the repo name
	if repoRoot, err := git.FindRoot(); err == nil {
		repoName := filepath.Base(repoRoot)
		spaceName = fmt.Sprintf("%s-%s", repoName, spaces.SlugifyBranch(spaceName))
	}

	return spaces.OpenSession(spaces.OpenSessionOptions{
		DestDir: dest,
		Name:    spaceName,
		Prompt:  promptFlag,
		Agent:   agentFlag,
	})
}

func runList(cmd *cobra.Command, args []string) error {
	dest, err := getPath()
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tBRANCH\tPORT\tSTATUS\tPATH")
	for _, e := range entries {
		branch := e.Branch
		if branch == "" {
			branch = "-"
		}
		status := "-"
		if tmux.SessionExists(e.Name) {
			status = "active"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", e.Name, branch, e.Port, status, e.Path)
	}
	w.Flush()
	return nil
}
