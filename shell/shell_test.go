package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/johanhenriksson/remux/registry"
)

func TestIsShell(t *testing.T) {
	for argv0, want := range map[string]bool{
		"remux-shell":                true,
		"-remux-shell":               true,
		"/usr/local/bin/remux-shell": true,
		"remux":                      false,
		"-zsh":                       false,
	} {
		if got := IsShell(argv0); got != want {
			t.Errorf("IsShell(%q) = %v, want %v", argv0, got, want)
		}
	}
}

func TestShellArgv0(t *testing.T) {
	if got := shellArgv0("/bin/zsh", true); got != "-zsh" {
		t.Errorf("login: got %q", got)
	}
	if got := shellArgv0("/bin/zsh", false); got != "zsh" {
		t.Errorf("non-login: got %q", got)
	}
}

func TestResolveShell(t *testing.T) {
	env := func(vals map[string]string) func(string) string {
		return func(k string) string { return vals[k] }
	}
	none := func() string { return "" }
	never := func(string) bool { return false }
	isWrapper := func(p string) bool { return IsShell(p) }

	// REMUX_SHELL wins
	if got := resolveShell(env(map[string]string{"REMUX_SHELL": "/bin/sh", "SHELL": "/bin/zsh"}), none, never); got != "/bin/sh" {
		t.Errorf("REMUX_SHELL: got %q", got)
	}
	// $SHELL pointing at the wrapper (as herdr sets it) is skipped for the user shell
	got := resolveShell(env(map[string]string{"SHELL": "/opt/bin/remux-shell"}), func() string { return "/bin/sh" }, isWrapper)
	if got != "/bin/sh" {
		t.Errorf("wrapper in SHELL: got %q", got)
	}
	// Relative or missing paths are ignored, falling back to a default
	got = resolveShell(env(map[string]string{"SHELL": "zsh"}), func() string { return "/nonexistent/shell" }, never)
	if got != "/bin/zsh" && got != "/bin/sh" {
		t.Errorf("fallback: got %q", got)
	}
}

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"A=1", "SHELL=/x/remux-shell", "B=2"}, map[string]string{"SHELL": "/bin/zsh", "C": "3"})
	want := []string{"A=1", "B=2", "C=3", "SHELL=/bin/zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestSpaceEnv(t *testing.T) {
	repo := t.TempDir()
	dest := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")
	cfg := "env:\n  APP_PORT: \"{{ space.Port + 1 }}\"\n"
	if err := os.WriteFile(filepath.Join(repo, ".remux.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "init")

	space := filepath.Join(dest, "repo-feat")
	unmanaged := filepath.Join(dest, "other")
	runGit(t, repo, "worktree", "add", "-q", "-b", "feat", space)
	runGit(t, repo, "worktree", "add", "-q", "-b", "other", unmanaged)
	if err := os.MkdirAll(filepath.Join(space, "sub", "dir"), 0755); err != nil {
		t.Fatal(err)
	}

	reg := &registry.Registry{}
	reg.Add("repo-feat", "feat", space, 11020, repo)
	if err := reg.Save(dest); err != nil {
		t.Fatal(err)
	}

	env, err := SpaceEnv(filepath.Join(space, "sub", "dir"))
	if err != nil {
		t.Fatal(err)
	}
	if env["APP_PORT"] != "11021" || env["SPACE_PORT"] != "11020" {
		t.Fatalf("space env: %v", env)
	}

	for name, dir := range map[string]string{"main checkout": repo, "unregistered worktree": unmanaged, "outside git": t.TempDir()} {
		env, err := SpaceEnv(dir)
		if err != nil || env != nil {
			t.Errorf("%s: expected no env, got %v %v", name, env, err)
		}
	}

	// A broken config in a real space is reported, not silently ignored
	if err := os.WriteFile(filepath.Join(space, ".remux.yaml"), []byte("env: [\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := SpaceEnv(space); err == nil {
		t.Fatal("expected config error")
	}
}
