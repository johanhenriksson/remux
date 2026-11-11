package spaces

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/johanhenriksson/remux/backend"
	"github.com/johanhenriksson/remux/config"
	"github.com/johanhenriksson/remux/registry"
)

// recordingBackend logs lifecycle calls into a shared event list so tests can
// check their order relative to hooks.
type recordingBackend struct {
	events *[]string
	open   bool
	env    map[string]string
	tabs   []config.Tab
}

func (r *recordingBackend) Name() string { return "fake" }
func (r *recordingBackend) Exists(backend.Space) (bool, error) {
	return r.open, nil
}
func (r *recordingBackend) Create(_ backend.Space, env map[string]string) error {
	*r.events = append(*r.events, "create")
	r.open, r.env = true, env
	return nil
}
func (r *recordingBackend) AddTabs(_ backend.Space, tabs []config.Tab, _ map[string]string) error {
	*r.events = append(*r.events, "tabs")
	r.tabs = tabs
	return nil
}
func (r *recordingBackend) Focus(backend.Space) error {
	*r.events = append(*r.events, "focus")
	return nil
}
func (r *recordingBackend) Close(backend.Space) error {
	*r.events = append(*r.events, "close")
	r.open = false
	return nil
}
func (r *recordingBackend) Current([]backend.Space) (*backend.Space, bool, error) {
	return nil, false, nil
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLifecycleWithBackend(t *testing.T) {
	repo := t.TempDir()
	dest := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")

	// Hooks append to a log file in the worktree so we can see when they ran
	cfg := `backend: fake
env_file: .env.local
env:
  APP_PORT: "{{ space.Port + 1 }}"
hooks:
  on_create:
    - echo create >> hooks.log
  on_open:
    - echo open >> hooks.log
  on_drop:
    - echo drop >> ` + filepath.Join(dest, "drop.log") + `
tabs:
  - name: server
    cmd: echo serve
`
	if err := os.WriteFile(filepath.Join(repo, ".remux.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "init")

	var events []string
	fake := &recordingBackend{events: &events}
	var gotName string
	newBackend = func(name string) (backend.Backend, error) {
		gotName = name
		return fake, nil
	}
	defer func() { newBackend = backend.New }()

	path, err := Create(CreateOptions{RepoRoot: repo, DestDir: dest, BranchName: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)

	reg, _ := registry.Load(dest)
	if !reg.Get(name).SetupPending {
		t.Fatal("expected setup to be pending after Create")
	}
	if _, err := os.Stat(filepath.Join(path, "hooks.log")); err == nil {
		t.Fatal("on_create must not run before the session exists")
	}

	opts := OpenSessionOptions{DestDir: dest, Name: name, Agent: "none", Detach: true, EnvVars: map[string]string{"EXTRA": "x"}}
	if err := OpenSession(opts); err != nil {
		t.Fatal(err)
	}
	if gotName != "fake" {
		t.Fatalf("backend from config: got %q", gotName)
	}
	if want := []string{"create", "tabs"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events: got %v want %v", events, want)
	}
	if fake.env["APP_PORT"] != "11011" || fake.env["SPACE_PORT"] != "11010" || fake.env["EXTRA"] != "x" {
		t.Fatalf("session env: %v", fake.env)
	}
	if len(fake.tabs) != 1 || fake.tabs[0].Name != "server" {
		t.Fatalf("tabs: %v", fake.tabs)
	}

	log, _ := os.ReadFile(filepath.Join(path, "hooks.log"))
	if string(log) != "create\nopen\n" {
		t.Fatalf("hooks.log: %q", log)
	}
	envFile, _ := os.ReadFile(filepath.Join(path, ".env.local"))
	if !strings.Contains(string(envFile), "APP_PORT=11011\n") || !strings.Contains(string(envFile), "SPACE_PORT=11010\n") {
		t.Fatalf(".env.local: %q", envFile)
	}
	if strings.Contains(string(envFile), "EXTRA") {
		t.Fatalf(".env.local should only hold the space env: %q", envFile)
	}
	reg, _ = registry.Load(dest)
	if reg.Get(name).SetupPending {
		t.Fatal("expected setup to be cleared after first open")
	}

	// Reopening an existing session runs on_open only and does not recreate
	events = nil
	if err := OpenSession(OpenSessionOptions{DestDir: dest, Name: name}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"focus"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("reopen events: got %v want %v", events, want)
	}

	// After close, opening recreates the session without on_create
	fake.open = false
	events = nil
	if err := OpenSession(OpenSessionOptions{DestDir: dest, Name: name, Agent: "none", Detach: true}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"create", "tabs"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("recreate events: got %v want %v", events, want)
	}
	log, _ = os.ReadFile(filepath.Join(path, "hooks.log"))
	if string(log) != "create\nopen\nopen\nopen\n" {
		t.Fatalf("hooks.log after reopen: %q", log)
	}

	// Drop closes the session before on_drop, then removes the worktree
	events = nil
	if err := Drop(path, true); err != nil {
		t.Fatal(err)
	}
	if want := []string{"close"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("drop events: got %v want %v", events, want)
	}
	if _, err := os.Stat(filepath.Join(dest, "drop.log")); err != nil {
		t.Fatal("on_drop did not run")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("worktree not removed")
	}
}

// A worktree adopted from another tool keeps its directory but is registered
// under a different name; its env (space.ID) must follow the registry name.
func TestAdoptedWorktree(t *testing.T) {
	repo := t.TempDir()
	dest := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")
	cfg := "backend: fake\nagent: none\nenv:\n  DB_NAME: \"{{ space.ID }}\"\nhooks:\n  on_drop:\n    - echo $DB_NAME > " + filepath.Join(dest, "dropped") + "\n"
	if err := os.WriteFile(filepath.Join(repo, ".remux.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "init")
	path := filepath.Join(dest, "me-abc-1-long-branch-name")
	runGit(t, repo, "worktree", "add", "-q", "-b", "me/abc-1-long-branch-name", path)

	reg := &registry.Registry{}
	reg.Add("repo-abc-1", "me/abc-1-long-branch-name", path, 12020, repo)
	if err := reg.Save(dest); err != nil {
		t.Fatal(err)
	}

	var events []string
	fake := &recordingBackend{events: &events, open: true}
	newBackend = func(string) (backend.Backend, error) { return fake, nil }
	defer func() { newBackend = backend.New }()

	space, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	env, err := space.SessionEnv()
	if err != nil {
		t.Fatal(err)
	}
	if space.Name != "repo-abc-1" || env["DB_NAME"] != "repo_abc_1" || env["SPACE_PORT"] != "12020" {
		t.Fatalf("adopted space: name=%q env=%v", space.Name, env)
	}

	// open by registry name finds the worktree despite the directory name
	if err := OpenSession(OpenSessionOptions{DestDir: dest, Name: "repo-abc-1", Detach: true}); err != nil {
		t.Fatal(err)
	}

	if err := Drop(path, true); err != nil {
		t.Fatal(err)
	}
	dropped, _ := os.ReadFile(filepath.Join(dest, "dropped"))
	if strings.TrimSpace(string(dropped)) != "repo_abc_1" {
		t.Fatalf("on_drop saw DB_NAME=%q", dropped)
	}
	reg, _ = registry.Load(dest)
	if len(reg.List()) != 0 {
		t.Fatalf("entry not unregistered: %v", reg.List())
	}
}
