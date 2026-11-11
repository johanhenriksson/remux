package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/johanhenriksson/remux/config"
)

// fakeHerdr records herdr invocations and replies with canned JSON keyed by
// the first two arguments (e.g. "worktree list").
type fakeHerdr struct {
	calls     [][]string
	responses map[string][]string // queued responses per command
}

func (f *fakeHerdr) run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	key := strings.Join(args[:min(len(args), 2)], " ")
	queue := f.responses[key]
	if len(queue) == 0 {
		return []byte(`{"result":{"type":"ok"}}`), nil
	}
	resp := queue[0]
	if len(queue) > 1 {
		f.responses[key] = queue[1:]
	}
	if strings.HasPrefix(resp, "error:") {
		return nil, fmt.Errorf("%s", strings.TrimPrefix(resp, "error:"))
	}
	return []byte(resp), nil
}

func (f *fakeHerdr) commands() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func newFake(responses map[string][]string) (*fakeHerdr, *herdrBackend) {
	f := &fakeHerdr{responses: responses}
	b := NewHerdr(f.run).(*herdrBackend)
	b.sleep = func(time.Duration) {}
	b.env = func(string) string { return "" }
	return f, b
}

func worktreeList(path, wsID string) string {
	id := "null"
	if wsID != "" {
		id = `"` + wsID + `"`
	}
	return fmt.Sprintf(`{"result":{"type":"worktree_list","worktrees":[
		{"path":"/repo","open_workspace_id":"w1"},
		{"path":%q,"open_workspace_id":%s}]}}`, path, id)
}

var testSpace = Space{Name: "repo-feat", Path: "/wt/repo-feat", RepoRoot: "/repo"}

func expectCalls(t *testing.T, f *fakeHerdr, want ...string) {
	t.Helper()
	if got := f.commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("herdr calls:\n got: %q\nwant: %q", got, want)
	}
}

func TestHerdrCreateReplacesRootTabWithEnvTab(t *testing.T) {
	f, b := newFake(map[string][]string{
		"worktree open": {`{"result":{"already_open":false,"workspace":{"workspace_id":"w9"},"tab":{"tab_id":"w9:t1"},"root_pane":{"pane_id":"w9:p1"}}}`},
		"tab create":    {`{"result":{"tab":{"tab_id":"w9:t2"},"root_pane":{"pane_id":"w9:p2"}}}`},
	})

	err := b.Create(testSpace, map[string]string{"PORT": "11010", "A": "1"})
	if err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f,
		"worktree open --cwd /repo --path /wt/repo-feat --no-focus",
		"tab create --workspace w9 --cwd /wt/repo-feat --label shell --env A=1 --env PORT=11010 --no-focus",
		"tab close w9:t1",
	)
}

func TestHerdrCreateWithoutEnvKeepsRootTab(t *testing.T) {
	f, b := newFake(map[string][]string{
		"worktree open": {`{"result":{"already_open":false,"workspace":{"workspace_id":"w9"},"tab":{"tab_id":"w9:t1"}}}`},
	})
	if err := b.Create(testSpace, nil); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f, "worktree open --cwd /repo --path /wt/repo-feat --no-focus")
}

func TestHerdrCreateAlreadyOpenLeavesWorkspaceAlone(t *testing.T) {
	f, b := newFake(map[string][]string{
		"worktree open": {`{"result":{"already_open":true,"workspace":{"workspace_id":"w9"},"tab":{"tab_id":"w9:t1"}}}`},
	})
	if err := b.Create(testSpace, map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f, "worktree open --cwd /repo --path /wt/repo-feat --no-focus")
}

func TestHerdrCreateError(t *testing.T) {
	_, b := newFake(map[string][]string{
		"worktree open": {"error:worktree_not_found"},
	})
	err := b.Create(testSpace, nil)
	if err == nil || !strings.Contains(err.Error(), "worktree_not_found") {
		t.Fatalf("expected worktree_not_found error, got %v", err)
	}
}

func TestHerdrExists(t *testing.T) {
	f, b := newFake(map[string][]string{
		"worktree list": {worktreeList("/wt/repo-feat", "w4"), worktreeList("/wt/repo-feat", "")},
	})

	ok, err := b.Exists(testSpace)
	if err != nil || !ok {
		t.Fatalf("expected open workspace, got %v %v", ok, err)
	}
	ok, err = b.Exists(testSpace)
	if err != nil || ok {
		t.Fatalf("expected closed workspace, got %v %v", ok, err)
	}
	if f.calls[0][3] != "/repo" {
		t.Fatalf("expected list --cwd /repo, got %q", f.calls[0])
	}
}

func TestHerdrExistsResolvesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}

	_, b := newFake(map[string][]string{
		"worktree list": {worktreeList(resolved, "w4")},
	})
	ok, err := b.Exists(Space{Name: "x", Path: link, RepoRoot: "/repo"})
	if err != nil || !ok {
		t.Fatalf("expected symlinked path to match, got %v %v", ok, err)
	}
}

func TestHerdrAddTabs(t *testing.T) {
	f, b := newFake(map[string][]string{
		"worktree list": {worktreeList("/wt/repo-feat", "w4")},
		"pane list":     {`{"result":{"panes":[{"pane_id":"w4:p2","tab_id":"w4:t2"}]}}`},
		"tab create":    {`{"result":{"tab":{"tab_id":"w4:t3"},"root_pane":{"pane_id":"w4:p3"}}}`},
	})

	tabs := []config.Tab{
		{Name: "claude", Cmd: "claude 'do it'"},
		{Name: "repl", Cmd: "python", Prompt: "print(1)", Await: ">>>", Timeout: 5},
	}
	if err := b.AddTabs(testSpace, tabs, map[string]string{"PORT": "1"}); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f,
		"worktree list --cwd /repo",
		"pane list --workspace w4",
		"tab rename w4:t2 claude",
		"pane run w4:p2 claude 'do it'",
		"tab create --workspace w4 --cwd /wt/repo-feat --label repl --env PORT=1 --no-focus",
		"pane run w4:p3 python",
		"pane wait-output w4:p3 --match >>> --timeout 5000",
		"pane send-text w4:p3 print(1)",
		"pane send-keys w4:p3 enter",
	)
}

func TestHerdrAddTabsRequiresWorkspace(t *testing.T) {
	_, b := newFake(map[string][]string{
		"worktree list": {worktreeList("/wt/repo-feat", "")},
	})
	if err := b.AddTabs(testSpace, []config.Tab{{Cmd: "ls"}}, nil); err == nil {
		t.Fatal("expected error when workspace is not open")
	}
}

func TestHerdrFocusAndClose(t *testing.T) {
	f, b := newFake(map[string][]string{
		"worktree list": {worktreeList("/wt/repo-feat", "w4"), worktreeList("/wt/repo-feat", "w4"), worktreeList("/wt/repo-feat", "")},
	})
	if err := b.Focus(testSpace); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(testSpace); err != nil {
		t.Fatal(err)
	}
	// Closing an already closed workspace is a no-op
	if err := b.Close(testSpace); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f,
		"worktree list --cwd /repo",
		"workspace focus w4",
		"worktree list --cwd /repo",
		"workspace close w4",
		"worktree list --cwd /repo",
	)
}

func TestHerdrCurrent(t *testing.T) {
	_, b := newFake(map[string][]string{
		"worktree list": {worktreeList("/wt/a", "w2"), worktreeList("/wt/b", "w3")},
	})

	cands := []Space{
		{Name: "a", Path: "/wt/a", RepoRoot: "/repo"},
		{Name: "b", Path: "/wt/b", RepoRoot: "/repo"},
	}

	cur, inside, err := b.Current(cands)
	if err != nil || inside || cur != nil {
		t.Fatalf("outside herdr: got %v %v %v", cur, inside, err)
	}

	b.env = func(k string) string {
		return map[string]string{"HERDR_ENV": "1", "HERDR_WORKSPACE_ID": "w3"}[k]
	}
	cur, inside, err = b.Current(cands)
	if err != nil || !inside || cur == nil || cur.Name != "b" {
		t.Fatalf("inside herdr: got %v %v %v", cur, inside, err)
	}
}

func TestNewBackend(t *testing.T) {
	t.Setenv(EnvVar, "")
	if b, err := New(""); err != nil || b.Name() != Tmux {
		t.Fatalf("default: got %v %v", b, err)
	}
	if b, err := New("herdr"); err != nil || b.Name() != Herdr {
		t.Fatalf("herdr: got %v %v", b, err)
	}
	t.Setenv(EnvVar, "herdr")
	if b, err := New(""); err != nil || b.Name() != Herdr {
		t.Fatalf("env default: got %v %v", b, err)
	}
	if b, err := New("tmux"); err != nil || b.Name() != Tmux {
		t.Fatalf("config overrides env: got %v %v", b, err)
	}
	if _, err := New("screen"); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func addTabsFake(extra map[string][]string) (*fakeHerdr, *herdrBackend) {
	responses := map[string][]string{
		"worktree list": {worktreeList("/wt/repo-feat", "w4")},
		"pane list":     {`{"result":{"panes":[{"pane_id":"w4:p2","tab_id":"w4:t2"}]}}`},
	}
	for k, v := range extra {
		responses[k] = v
	}
	return newFake(responses)
}

func TestHerdrAgentTabPastesPrompt(t *testing.T) {
	f, b := addTabsFake(nil)
	long := strings.Repeat("do the thing. ", 500) // a delegation-sized prompt
	tabs := []config.Tab{{Name: "claude", Cmd: "claude --permission-mode auto", Prompt: long, AgentName: "ait-1"}}
	if err := b.AddTabs(testSpace, tabs, nil); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f,
		"worktree list --cwd /repo",
		"pane list --workspace w4",
		"tab rename w4:t2 claude",
		"agent start ait-1 --kind claude --pane w4:p2 -- --permission-mode auto",
		"agent prompt ait-1 "+strings.TrimSpace(long),
	)
}

func TestHerdrAgentTabWaitsWhenBlocked(t *testing.T) {
	f, b := addTabsFake(map[string][]string{
		"agent start": {`error:{"error":{"code":"agent_not_ready"}}`},
	})
	tabs := []config.Tab{{Name: "codex", Cmd: "codex", Prompt: "go", AgentName: "feat"}}
	if err := b.AddTabs(testSpace, tabs, nil); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, f,
		"worktree list --cwd /repo",
		"pane list --workspace w4",
		"tab rename w4:t2 codex",
		"agent start feat --kind codex --pane w4:p2",
		"agent wait feat --until idle --until done --timeout 120000",
		"agent prompt feat go",
	)
}

func TestHerdrAgentTabBlockedWithoutPrompt(t *testing.T) {
	f, b := addTabsFake(map[string][]string{
		"agent start": {`error:agent_not_ready`},
	})
	tabs := []config.Tab{{Name: "claude", Cmd: "claude", AgentName: "feat"}}
	if err := b.AddTabs(testSpace, tabs, nil); err != nil {
		t.Fatal(err)
	}
	if last := f.commands()[len(f.calls)-1]; last != "agent start feat --kind claude --pane w4:p2" {
		t.Fatalf("expected no wait without a prompt, last call %q", last)
	}
}

func TestHerdrAgentStartError(t *testing.T) {
	_, b := addTabsFake(map[string][]string{
		"agent start": {`error:agent_name_taken`},
	})
	tabs := []config.Tab{{Name: "claude", Cmd: "claude", Prompt: "go", AgentName: "feat"}}
	if err := b.AddTabs(testSpace, tabs, nil); err == nil || !strings.Contains(err.Error(), "agent_name_taken") {
		t.Fatalf("expected start error, got %v", err)
	}
}

func TestHerdrUnknownAgentCommandRunsInline(t *testing.T) {
	f, b := addTabsFake(nil)
	tabs := []config.Tab{{Name: "aider", Cmd: "aider --yes", Prompt: "hi", AgentName: "feat"}}
	if err := b.AddTabs(testSpace, tabs, nil); err != nil {
		t.Fatal(err)
	}
	got := f.commands()
	if got[3] != "pane run w4:p2 aider --yes" || got[4] != "pane send-text w4:p2 hi" {
		t.Fatalf("unexpected calls %q", got)
	}
}
