package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func writeWorkflow(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadWorkflow(t *testing.T) {
	content := `---
tracker:
  kind: linear
  api_key: $LINEAR_API_KEY
  project_slug: test-project
  terminal_states: ["Done", "Cancelled"]
polling:
  interval_ms: 10000
agent:
  command: "claude --dangerously-skip-permissions"
  max_concurrent: 2
  max_retry_backoff_ms: 60000
workflow:
  todo:
    trigger_status: Todo
    next_status: Planned
    progress_status: In Progress
  ready:
    trigger_status: Ready
    next_status: Review
    progress_status: In Progress
---
You are working on issue {{.Identifier}}: {{.Title}}

{{.Description}}
`
	t.Setenv("LINEAR_API_KEY", "test-key-123")
	path := writeWorkflow(t, content)

	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}

	if wf.Config.Tracker.Kind != "linear" {
		t.Errorf("kind = %q, want linear", wf.Config.Tracker.Kind)
	}
	if wf.Config.Tracker.APIKey != "test-key-123" {
		t.Errorf("api_key = %q, want test-key-123", wf.Config.Tracker.APIKey)
	}
	if wf.Config.Tracker.ProjectSlug != "test-project" {
		t.Errorf("project_slug = %q, want test-project", wf.Config.Tracker.ProjectSlug)
	}
	if wf.Config.Polling.Interval != 10*time.Second {
		t.Errorf("interval = %v, want 10s", wf.Config.Polling.Interval)
	}
	if wf.Config.Agent.MaxConcurrent != 2 {
		t.Errorf("max_concurrent = %d, want 2", wf.Config.Agent.MaxConcurrent)
	}
	if wf.Config.Agent.MaxRetryBackoff != 60*time.Second {
		t.Errorf("max_retry_backoff = %v, want 60s", wf.Config.Agent.MaxRetryBackoff)
	}

	// Workflow steps
	if len(wf.Config.Tracker.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(wf.Config.Tracker.Steps))
	}
	todo := wf.Config.Tracker.Steps["todo"]
	if todo.TriggerStatus != "Todo" || len(todo.NextStatus) != 1 || todo.NextStatus[0] != "Planned" || todo.ProgressStatus != "In Progress" {
		t.Errorf("todo step = %+v", todo)
	}
	ready := wf.Config.Tracker.Steps["ready"]
	if ready.TriggerStatus != "Ready" || len(ready.NextStatus) != 1 || ready.NextStatus[0] != "Review" || ready.ProgressStatus != "In Progress" {
		t.Errorf("ready step = %+v", ready)
	}

	// Template rendering
	issue := Issue{
		Identifier:  "ABC-123",
		Title:       "Fix login bug",
		Description: "Users can't log in",
	}
	attempt := 1
	prompt, err := wf.RenderPrompt(issue, "todo", &attempt, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "You are working on issue ABC-123: Fix login bug"; !contains(prompt, want) {
		t.Errorf("prompt missing %q, got: %s", want, prompt)
	}
	if want := "Users can't log in"; !contains(prompt, want) {
		t.Errorf("prompt missing %q, got: %s", want, prompt)
	}
}

func TestActiveStates(t *testing.T) {
	tc := TrackerConfig{
		Steps: map[string]WorkflowStep{
			"todo":  {TriggerStatus: "Todo"},
			"ready": {TriggerStatus: "Ready"},
			"merge": {TriggerStatus: "Merge"},
		},
	}
	got := tc.ActiveStates()
	sort.Strings(got)
	want := []string{"Merge", "Ready", "Todo"}
	if len(got) != len(want) {
		t.Fatalf("ActiveStates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ActiveStates()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStepForState(t *testing.T) {
	tc := TrackerConfig{
		Steps: map[string]WorkflowStep{
			"todo":  {TriggerStatus: "Todo", NextStatus: []string{"Planned"}, ProgressStatus: "In Progress"},
			"ready": {TriggerStatus: "Ready", NextStatus: []string{"Review"}, ProgressStatus: "In Progress"},
		},
	}

	key, step, ok := tc.StepForState("Todo")
	if !ok || key != "todo" || len(step.NextStatus) != 1 || step.NextStatus[0] != "Planned" {
		t.Errorf("StepForState(Todo) = %q, %+v, %v", key, step, ok)
	}

	// case insensitive
	key, step, ok = tc.StepForState("todo")
	if !ok || key != "todo" {
		t.Errorf("StepForState(todo) case-insensitive failed")
	}

	_, _, ok = tc.StepForState("Unknown")
	if ok {
		t.Error("StepForState(Unknown) should return false")
	}
}

func TestLoadWorkflowNextStatusList(t *testing.T) {
	content := `---
tracker:
  api_key: $LINEAR_API_KEY
  project_slug: test-project
workflow:
  implement:
    trigger_status: Todo
    next_status: ["Review", "Ready"]
    progress_status: In Progress
---
Do work on {{.Identifier}}
`
	t.Setenv("LINEAR_API_KEY", "key")
	path := writeWorkflow(t, content)

	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}

	step := wf.Config.Tracker.Steps["implement"]
	if len(step.NextStatus) != 2 || step.NextStatus[0] != "Review" || step.NextStatus[1] != "Ready" {
		t.Errorf("next_status = %v, want [Review Ready]", step.NextStatus)
	}
}

func TestLoadWorkflowLabels(t *testing.T) {
	content := `---
tracker:
  api_key: $LINEAR_API_KEY
  project_slug: test-project
  labels: ["Claudable", "Urgent"]
workflow:
  todo:
    trigger_status: Todo
    next_status: Planned
    progress_status: In Progress
---
Do work on {{.Identifier}}
`
	t.Setenv("LINEAR_API_KEY", "key")
	path := writeWorkflow(t, content)

	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(wf.Config.Tracker.Labels) != 2 || wf.Config.Tracker.Labels[0] != "Claudable" || wf.Config.Tracker.Labels[1] != "Urgent" {
		t.Errorf("labels = %v, want [Claudable Urgent]", wf.Config.Tracker.Labels)
	}
}

func TestLoadWorkflowMissingWorkflow(t *testing.T) {
	content := `---
tracker:
  api_key: $LINEAR_API_KEY
  project_slug: test-project
---
Do work on {{.Identifier}}
`
	t.Setenv("LINEAR_API_KEY", "key")
	path := writeWorkflow(t, content)

	_, err := LoadWorkflow(path)
	if err == nil {
		t.Fatal("expected error for missing workflow steps")
	}
}

func TestLoadWorkflowMissingProjectSlug(t *testing.T) {
	content := `---
tracker:
  api_key: $LINEAR_API_KEY
workflow:
  todo:
    trigger_status: Todo
    next_status: Planned
    progress_status: In Progress
---
prompt
`
	t.Setenv("LINEAR_API_KEY", "key")
	path := writeWorkflow(t, content)

	_, err := LoadWorkflow(path)
	if err == nil {
		t.Fatal("expected error for missing project_slug")
	}
}

func TestLoadWorkflowNoFrontMatter(t *testing.T) {
	content := `Just a plain prompt without front matter`
	path := writeWorkflow(t, content)

	_, err := LoadWorkflow(path)
	if err == nil {
		t.Fatal("expected error for missing front matter")
	}
}

func TestRetryDelay(t *testing.T) {
	max := 5 * time.Minute

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{3, 40 * time.Second},
		{4, 80 * time.Second},
		{10, max},
	}

	for _, tc := range cases {
		got := retryDelay(tc.attempt, max)
		if got != tc.want {
			t.Errorf("retryDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) && (s == substr || findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
