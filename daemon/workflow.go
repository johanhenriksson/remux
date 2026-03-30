package daemon

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

// WorkflowStep defines a single step in the issue workflow.
type WorkflowStep struct {
	Name           string // display name for this step
	TriggerStatus  string // linear state name that triggers this step
	NextStatus     string // where to move on success
	ProgressStatus string // where to move when work starts
}

// WorkflowConfig holds typed configuration extracted from WORKFLOW.md front matter.
type WorkflowConfig struct {
	Tracker  TrackerConfig
	Polling  PollingConfig
	Agent    AgentConfig
}

type TrackerConfig struct {
	Kind           string
	APIKey         string
	Endpoint       string
	ProjectSlug    string
	Steps          map[string]WorkflowStep
	TerminalStates []string
	Labels         []string
}

// ActiveStates returns the list of Linear state names that the daemon picks up.
func (tc *TrackerConfig) ActiveStates() []string {
	states := make([]string, 0, len(tc.Steps))
	for _, step := range tc.Steps {
		states = append(states, step.TriggerStatus)
	}
	return states
}

// StepForState returns the step key and config for a given Linear state name.
func (tc *TrackerConfig) StepForState(stateName string) (string, WorkflowStep, bool) {
	for key, step := range tc.Steps {
		if strings.EqualFold(step.TriggerStatus, stateName) {
			return key, step, true
		}
	}
	return "", WorkflowStep{}, false
}

type PollingConfig struct {
	Interval time.Duration
}

type AgentConfig struct {
	Command         string
	MaxConcurrent   int
	MaxRetryBackoff time.Duration
	IdleTimeout     time.Duration
}

// Workflow represents a parsed WORKFLOW.md file.
type Workflow struct {
	Config   WorkflowConfig
	template *template.Template
}

// LoadWorkflow parses a WORKFLOW.md file, extracting YAML front matter and the prompt template.
func LoadWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow file: %w", err)
	}

	frontMatter, promptBody, err := splitFrontMatter(string(data))
	if err != nil {
		return nil, err
	}

	cfg, err := parseConfig(frontMatter)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(promptBody)
	if err != nil {
		return nil, fmt.Errorf("parse prompt template: %w", err)
	}

	return &Workflow{Config: cfg, template: tmpl}, nil
}

// RenderPrompt renders the prompt template with the given issue, step name, and attempt number.
func (w *Workflow) RenderPrompt(issue Issue, stepName string, attempt *int, milestoneBranch string) (string, error) {
	data := map[string]any{
		"ID":              issue.ID,
		"Identifier":      issue.Identifier,
		"Title":           issue.Title,
		"Description":     issue.Description,
		"Priority":        issue.Priority,
		"State":           issue.State,
		"StepName":        stepName,
		"BranchName":      issue.BranchName,
		"URL":             issue.URL,
		"Labels":          issue.Labels,
		"Attempt":         attempt,
		"Milestone":       issue.Milestone,
		"MilestoneBranch": milestoneBranch,
	}

	var buf bytes.Buffer
	if err := w.template.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}
	return buf.String(), nil
}

func splitFrontMatter(content string) (map[string]any, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, "", fmt.Errorf("workflow file must start with YAML front matter (---)")
	}

	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return nil, "", fmt.Errorf("unterminated YAML front matter")
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(rest[:idx]), &fm); err != nil {
		return nil, "", fmt.Errorf("parse front matter YAML: %w", err)
	}

	promptBody := strings.TrimSpace(rest[idx+5:])
	return fm, promptBody, nil
}

func parseConfig(fm map[string]any) (WorkflowConfig, error) {
	cfg := WorkflowConfig{
		Tracker: TrackerConfig{
			Kind:           "linear",
			Endpoint:       "https://api.linear.app/graphql",
			TerminalStates: []string{"Done", "Cancelled", "Closed", "Canceled", "Duplicate"},
		},
		Polling: PollingConfig{
			Interval: 30 * time.Second,
		},
		Agent: AgentConfig{
			Command:         "claude --dangerously-skip-permissions",
			MaxConcurrent:   3,
			MaxRetryBackoff: 5 * time.Minute,
			IdleTimeout:     15 * time.Minute,
		},
	}

	if tracker, ok := fm["tracker"].(map[string]any); ok {
		if v, ok := tracker["kind"].(string); ok {
			cfg.Tracker.Kind = v
		}
		if v, ok := tracker["api_key"].(string); ok {
			cfg.Tracker.APIKey = resolveEnvVar(v)
		}
		if v, ok := tracker["endpoint"].(string); ok {
			cfg.Tracker.Endpoint = v
		}
		if v, ok := tracker["project_slug"].(string); ok {
			cfg.Tracker.ProjectSlug = v
		}
		if v, ok := tracker["terminal_states"].([]any); ok {
			cfg.Tracker.TerminalStates = toStringSlice(v)
		}
		if v, ok := tracker["labels"].([]any); ok {
			cfg.Tracker.Labels = toStringSlice(v)
		}
	}

	if workflow, ok := fm["workflow"].(map[string]any); ok {
		cfg.Tracker.Steps = make(map[string]WorkflowStep, len(workflow))
		for key, val := range workflow {
			stepMap, ok := val.(map[string]any)
			if !ok {
				continue
			}
			step := WorkflowStep{}
			if v, ok := stepMap["name"].(string); ok {
				step.Name = v
			}
			if v, ok := stepMap["trigger_status"].(string); ok {
				step.TriggerStatus = v
			}
			if v, ok := stepMap["next_status"].(string); ok {
				step.NextStatus = v
			}
			if v, ok := stepMap["progress_status"].(string); ok {
				step.ProgressStatus = v
			}
			if step.Name == "" {
				step.Name = key
			}
			if step.TriggerStatus == "" {
				return cfg, fmt.Errorf("workflow step %q: trigger_status is required", key)
			}
			cfg.Tracker.Steps[key] = step
		}
	}

	if polling, ok := fm["polling"].(map[string]any); ok {
		if v, ok := toInt(polling["interval_ms"]); ok {
			cfg.Polling.Interval = time.Duration(v) * time.Millisecond
		}
	}

	if agent, ok := fm["agent"].(map[string]any); ok {
		if v, ok := agent["command"].(string); ok {
			cfg.Agent.Command = v
		}
		if v, ok := toInt(agent["max_concurrent"]); ok {
			cfg.Agent.MaxConcurrent = v
		}
		if v, ok := toInt(agent["max_retry_backoff_ms"]); ok {
			cfg.Agent.MaxRetryBackoff = time.Duration(v) * time.Millisecond
		}
		if v, ok := toInt(agent["idle_timeout"]); ok {
			cfg.Agent.IdleTimeout = time.Duration(v) * time.Minute
		}
	}

	if cfg.Tracker.Kind != "linear" {
		return cfg, fmt.Errorf("unsupported tracker kind: %q", cfg.Tracker.Kind)
	}
	if cfg.Tracker.ProjectSlug == "" {
		return cfg, fmt.Errorf("tracker.project_slug is required")
	}
	if cfg.Tracker.APIKey == "" {
		return cfg, fmt.Errorf("tracker.api_key is required (use $ENV_VAR syntax)")
	}
	if len(cfg.Tracker.Steps) == 0 {
		return cfg, fmt.Errorf("workflow must define at least one step")
	}

	return cfg, nil
}

// resolveEnvVar expands a $VAR reference from the environment.
func resolveEnvVar(s string) string {
	if strings.HasPrefix(s, "$") {
		return os.Getenv(s[1:])
	}
	return s
}

func toStringSlice(v []any) []string {
	out := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
