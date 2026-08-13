package daemon

import (
	"reflect"
	"testing"
)

func TestBuildAgentInvocationClaude(t *testing.T) {
	issue := Issue{
		Identifier: "ABC-123",
		Labels:     []string{"max"},
	}

	got, err := buildAgentInvocation("claude --dangerously-skip-permissions", issue, "do work", "session-1", "ABC-123 Ready")
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != agentKindClaude {
		t.Fatalf("kind = %q, want %q", got.kind, agentKindClaude)
	}

	want := []string{
		"claude", "--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--verbose",
		"--remote-control", "ABC-123",
		"--session-id", "session-1",
		"--name", "ABC-123 Ready",
		"--effort", "max",
		"-p", "do work",
	}
	if !reflect.DeepEqual(got.parts, want) {
		t.Fatalf("parts = %#v, want %#v", got.parts, want)
	}
}

func TestBuildAgentInvocationCodexDefaults(t *testing.T) {
	got, err := buildAgentInvocation("codex", Issue{}, "do work", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != agentKindCodex {
		t.Fatalf("kind = %q, want %q", got.kind, agentKindCodex)
	}

	want := []string{"codex", "exec", "--json", "--", "do work"}
	if !reflect.DeepEqual(got.parts, want) {
		t.Fatalf("parts = %#v, want %#v", got.parts, want)
	}
}

func TestBuildAgentInvocationCodexPreservesExecJSONAndQuotedConfig(t *testing.T) {
	command := `codex exec --json --model gpt-5 -c 'sandbox_permissions=["workspace-write"]'`
	got, err := buildAgentInvocation(command, Issue{}, "do work", "", "")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"codex", "exec", "--json",
		"--model", "gpt-5",
		"-c", `sandbox_permissions=["workspace-write"]`,
		"--", "do work",
	}
	if !reflect.DeepEqual(got.parts, want) {
		t.Fatalf("parts = %#v, want %#v", got.parts, want)
	}
}

func TestBuildAgentInvocationCodexWithGlobalOptions(t *testing.T) {
	got, err := buildAgentInvocation("codex --profile work", Issue{}, "do work", "", "")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"codex", "--profile", "work", "exec", "--json", "--", "do work"}
	if !reflect.DeepEqual(got.parts, want) {
		t.Fatalf("parts = %#v, want %#v", got.parts, want)
	}
}

func TestSplitCommandLineRejectsUnterminatedQuote(t *testing.T) {
	if _, err := splitCommandLine("codex exec 'unterminated"); err == nil {
		t.Fatal("expected unterminated quote error")
	}
}

func TestCodexEventTexts(t *testing.T) {
	event := map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "output_text", "text": "done"},
			},
		},
	}

	got := codexEventTexts(event)
	want := []string{"done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

func TestCodexEventTextsAgentMessage(t *testing.T) {
	event := map[string]any{
		"type":    "agent_message",
		"message": "done",
	}

	got := codexEventTexts(event)
	want := []string{"done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

func TestCodexEventTextsSkipsUserMessages(t *testing.T) {
	event := map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "prompt"},
			},
		},
	}

	if got := codexEventTexts(event); len(got) != 0 {
		t.Fatalf("texts = %#v, want empty", got)
	}
}

func TestCodexEventToolName(t *testing.T) {
	event := map[string]any{
		"type":       "exec_command_begin",
		"parsed_cmd": "go test ./...",
	}

	if got := codexEventToolName(event); got != "go test ./..." {
		t.Fatalf("tool = %q, want command", got)
	}
}
