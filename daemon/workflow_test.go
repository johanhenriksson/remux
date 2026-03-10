package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadWorkflow(t *testing.T) {
	content := `---
tracker:
  kind: linear
  api_key: $LINEAR_API_KEY
  project_slug: test-project
  active_states: ["Todo", "In Progress"]
  terminal_states: ["Done", "Cancelled"]
polling:
  interval_ms: 10000
agent:
  command: "claude --dangerously-skip-permissions"
  max_concurrent: 2
  max_retry_backoff_ms: 60000
---
You are working on issue {{.Identifier}}: {{.Title}}

{{.Description}}
`
	t.Setenv("LINEAR_API_KEY", "test-key-123")

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}

	// Config
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

	// Template rendering
	issue := Issue{
		Identifier:  "ABC-123",
		Title:       "Fix login bug",
		Description: "Users can't log in",
	}
	attempt := 1
	prompt, err := wf.RenderPrompt(issue, &attempt)
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

func TestLoadWorkflowDefaults(t *testing.T) {
	content := `---
tracker:
  api_key: $LINEAR_API_KEY
  project_slug: my-project
---
Do work on {{.Identifier}}
`
	t.Setenv("LINEAR_API_KEY", "key")

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}

	if wf.Config.Polling.Interval != 30*time.Second {
		t.Errorf("default interval = %v, want 30s", wf.Config.Polling.Interval)
	}
	if wf.Config.Agent.MaxConcurrent != 3 {
		t.Errorf("default max_concurrent = %d, want 3", wf.Config.Agent.MaxConcurrent)
	}
	if wf.Config.Agent.Command != "claude --dangerously-skip-permissions" {
		t.Errorf("default command = %q", wf.Config.Agent.Command)
	}
}

func TestLoadWorkflowMissingProjectSlug(t *testing.T) {
	content := `---
tracker:
  api_key: $LINEAR_API_KEY
---
prompt
`
	t.Setenv("LINEAR_API_KEY", "key")

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadWorkflow(path)
	if err == nil {
		t.Fatal("expected error for missing project_slug")
	}
}

func TestLoadWorkflowNoFrontMatter(t *testing.T) {
	content := `Just a plain prompt without front matter`

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

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
		{10, max}, // capped
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
