package spaces

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johanhenriksson/remux/backend"
	"github.com/johanhenriksson/remux/config"
)

// openWithAgent creates a space from a repo with the given .remux.yaml, opens
// it with agent against a fake backend and returns the space path and the
// agent tab (or nil when there was no agent tab).
func openWithAgent(t *testing.T, cfg, agent string) (string, *config.Tab) {
	t.Helper()
	repo := t.TempDir()
	dest := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, ".remux.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "init")

	var events []string
	fake := &recordingBackend{events: &events}
	newBackend = func(string) (backend.Backend, error) { return fake, nil }
	t.Cleanup(func() { newBackend = backend.New })

	path, err := Create(CreateOptions{RepoRoot: repo, DestDir: dest, BranchName: "feat"})
	if err != nil {
		t.Fatal(err)
	}
	opts := OpenSessionOptions{DestDir: dest, Name: filepath.Base(path), Agent: agent, Prompt: "do it", Detach: true}
	if err := OpenSession(opts); err != nil {
		t.Fatal(err)
	}
	for i := range fake.tabs {
		if fake.tabs[i].AgentName != "" {
			return path, &fake.tabs[i]
		}
	}
	return path, nil
}

const instructionsConfig = `backend: fake
env:
  API_PORT: "{{ space.Port + 1 }}"
instructions: |
  API runs on port {{ env.API_PORT }}.
`

func TestClaudeGetsInstructionsFile(t *testing.T) {
	path, tab := openWithAgent(t, instructionsConfig, "claude")
	if tab == nil {
		t.Fatal("no agent tab")
	}
	if tab.Cmd != "claude --permission-mode auto" || tab.Prompt != "do it" {
		t.Fatalf("agent tab changed: cmd=%q prompt=%q", tab.Cmd, tab.Prompt)
	}
	if len(tab.AgentArgs) != 2 || tab.AgentArgs[0] != "--append-system-prompt-file" {
		t.Fatalf("agent args: %q", tab.AgentArgs)
	}
	file := tab.AgentArgs[1]
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "You are running in a remux workspace") ||
		!strings.Contains(text, "- Workspace: "+filepath.Base(path)+" at "+path+"\n") ||
		!strings.HasSuffix(text, "operations.\n\nAPI runs on port 11011.\n") {
		t.Fatalf("instructions file:\n%s", text)
	}

	// The file lives in the worktree's git dir, out of git status
	if strings.HasPrefix(file, path+string(filepath.Separator)) {
		t.Fatalf("instructions file inside the worktree: %s", file)
	}
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil || len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("git status not clean: %q %v", out, err)
	}
}

func TestClaudeWithFlagsGetsInstructions(t *testing.T) {
	_, tab := openWithAgent(t, "backend: fake\n", "claude --model opus")
	if len(tab.AgentArgs) != 2 || tab.AgentArgs[0] != "--append-system-prompt-file" {
		t.Fatalf("agent args: %q", tab.AgentArgs)
	}
}

func TestCodexGetsDeveloperInstructions(t *testing.T) {
	_, tab := openWithAgent(t, instructionsConfig, "codex")
	if len(tab.AgentArgs) != 2 || tab.AgentArgs[0] != "-c" {
		t.Fatalf("agent args: %q", tab.AgentArgs)
	}
	arg := tab.AgentArgs[1]
	if !strings.HasPrefix(arg, `developer_instructions="You are running in a remux workspace`) ||
		!strings.HasSuffix(arg, `operations.\n\nAPI runs on port 11011."`) {
		t.Fatalf("codex arg: %s", arg)
	}
	if strings.ContainsAny(arg, "\n\r") {
		t.Fatalf("codex arg must be a single line: %q", arg)
	}
}

func TestUnknownAgentGetsNoInstructions(t *testing.T) {
	_, tab := openWithAgent(t, instructionsConfig, "aider --yes")
	if tab == nil || tab.Cmd != "aider --yes" || len(tab.AgentArgs) != 0 {
		t.Fatalf("unknown agent tab: %+v", tab)
	}
}

func TestNoInstructionsWhenDisabledAndEmpty(t *testing.T) {
	_, tab := openWithAgent(t, "backend: fake\ndefault_instructions: false\n", "claude")
	if len(tab.AgentArgs) != 0 {
		t.Fatalf("agent args: %q", tab.AgentArgs)
	}
}

func TestTomlString(t *testing.T) {
	got := tomlString("a \"b\" \\ c\nd\te\x01")
	want := `"a \"b\" \\ c\nd\te\u0001"`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
