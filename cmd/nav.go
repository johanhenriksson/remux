package cmd

import (
	"fmt"
	"sort"

	"github.com/johanhenriksson/remux/backend"
	"github.com/johanhenriksson/remux/registry"
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
		return navigate(direction)
	}
}

// navigate switches to the next or previous active remux session in the
// backend the caller runs inside. direction should be +1 or -1.
func navigate(direction int) error {
	dest, err := getPath()
	if err != nil {
		return err
	}

	b := backend.Detect()
	targets, err := activeRemuxSessions(b, dest)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		fmt.Println("No active remux sessions")
		return nil
	}

	current, inside, err := b.Current(targets)
	if err != nil {
		return err
	}

	if !inside {
		// Outside the backend: attach to first (next) or last (prev) session
		target := targets[0]
		if direction < 0 {
			target = targets[len(targets)-1]
		}
		return b.Focus(target)
	}

	if len(targets) == 1 {
		fmt.Println("Only one active remux session")
		return nil
	}

	// Find current session index
	index := -1
	if current != nil {
		for i, t := range targets {
			if t.Name == current.Name {
				index = i
				break
			}
		}
	}

	var next backend.Space
	if index < 0 {
		// Current session isn't a remux session; jump to first
		next = targets[0]
	} else {
		next = targets[(index+direction+len(targets))%len(targets)]
	}

	return b.Focus(next)
}

// activeRemuxSessions returns registry entries that have a live session in
// the given backend, sorted by name.
func activeRemuxSessions(b backend.Backend, destDir string) ([]backend.Space, error) {
	reg, err := registry.Load(destDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	var targets []backend.Space
	for _, entry := range reg.List() {
		space := backend.Space{Name: entry.Name, Path: entry.Path, RepoRoot: entry.RepoRoot}
		// Entries the backend can't inspect (e.g. deleted repos) count as inactive
		if active, err := b.Exists(space); err == nil && active {
			targets = append(targets, space)
		}
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}
