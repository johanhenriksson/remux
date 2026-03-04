package cmd

import (
	"fmt"
	"sort"

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/tmux"
	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Switch to the next active remux session",
	Args:  cobra.NoArgs,
	RunE:  runNav(1),
}

var prevCmd = &cobra.Command{
	Use:   "prev",
	Short: "Switch to the previous active remux session",
	Args:  cobra.NoArgs,
	RunE:  runNav(-1),
}

func init() {
	rootCmd.AddCommand(nextCmd)
	rootCmd.AddCommand(prevCmd)
}

// runNav returns a cobra RunE function that navigates to the next or previous
// active remux session. direction should be +1 or -1.
func runNav(direction int) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		dest, err := getPath()
		if err != nil {
			return err
		}

		targets, err := activeRemuxSessions(dest)
		if err != nil {
			return err
		}

		if len(targets) == 0 {
			fmt.Println("No active remux sessions")
			return nil
		}

		if !tmux.InSession() {
			// Outside tmux: attach to first (next) or last (prev) session
			target := targets[0]
			if direction < 0 {
				target = targets[len(targets)-1]
			}
			return tmux.Attach(target)
		}

		if len(targets) == 1 {
			fmt.Println("Only one active remux session")
			return nil
		}

		current, err := tmux.CurrentSession()
		if err != nil {
			return fmt.Errorf("failed to get current session: %w", err)
		}

		// Find current session index
		index := -1
		for i, name := range targets {
			if name == current {
				index = i
				break
			}
		}

		var next string
		if index < 0 {
			// Current session isn't a remux session; jump to first
			next = targets[0]
		} else {
			next = targets[(index+direction+len(targets))%len(targets)]
		}

		return tmux.SwitchTo(next)
	}
}

// activeRemuxSessions returns sorted sanitized names of registry entries
// that have an active tmux session.
func activeRemuxSessions(destDir string) ([]string, error) {
	reg, err := registry.Load(destDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	sessions, err := tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("failed to list tmux sessions: %w", err)
	}

	active := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		active[s] = true
	}

	var targets []string
	for _, entry := range reg.List() {
		name := tmux.SessionName(entry.Name)
		if active[name] {
			targets = append(targets, name)
		}
	}

	sort.Strings(targets)
	return targets, nil
}
