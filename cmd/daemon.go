package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/johanhenriksson/remux/daemon"
	"github.com/johanhenriksson/remux/git"
	"github.com/spf13/cobra"
)

var daemonPathDir string
var daemonDebug bool

var daemonCmd = &cobra.Command{
	Use:   "daemon [workflow-file]",
	Short: "Run the orchestrator daemon",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDaemon,
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.Flags().StringVar(&daemonPathDir, "path", "", "worktree directory (default: ~/.remux)")
	daemonCmd.Flags().BoolVar(&daemonDebug, "debug", false, "enable verbose debug logging")
}

func runDaemon(cmd *cobra.Command, args []string) error {
	workflowPath := "./WORKFLOW.md"
	if len(args) > 0 {
		workflowPath = args[0]
	}

	repoRoot, err := git.FindRoot()
	if err != nil {
		return err
	}

	destDir, err := resolveDestDir(daemonPathDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received %s, shutting down...", sig)
		cancel()
	}()

	orch := daemon.New(workflowPath, repoRoot, destDir, daemonDebug)
	return orch.Run(ctx)
}
