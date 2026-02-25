package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/tmux"
)

func TestActiveRemuxSessions(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	// Create temp dir for registry
	dir, err := os.MkdirTemp("", "nav-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create a registry with two entries
	reg := &registry.Registry{}
	reg.Add("space-a", filepath.Join(dir, "a"), 11010, dir)
	reg.Add("space-b", filepath.Join(dir, "b"), 11020, dir)
	reg.Add("space-c", filepath.Join(dir, "c"), 11030, dir)
	if err := reg.Save(dir); err != nil {
		t.Fatal(err)
	}

	// Create tmux sessions for a and b only
	workdir, _ := os.Getwd()
	tmux.NewSessionDetached("space-a", workdir, nil)
	tmux.NewSessionDetached("space-b", workdir, nil)
	defer tmux.KillSession("space-a")
	defer tmux.KillSession("space-b")

	sessions, err := activeRemuxSessions(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should only include space-a and space-b (sorted), not space-c
	if len(sessions) != 2 {
		t.Fatalf("expected 2 active sessions, got %d: %v", len(sessions), sessions)
	}
	if sessions[0] != "space-a" || sessions[1] != "space-b" {
		t.Fatalf("expected [space-a space-b], got %v", sessions)
	}
}

func TestActiveRemuxSessionsEmpty(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	dir, err := os.MkdirTemp("", "nav-test-empty-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Empty registry
	reg := &registry.Registry{}
	if err := reg.Save(dir); err != nil {
		t.Fatal(err)
	}

	sessions, err := activeRemuxSessions(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}
