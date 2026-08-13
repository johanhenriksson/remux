package spaces

import (
	"testing"

	"github.com/johanhenriksson/remux/config"
)

func TestInlinePrompt(t *testing.T) {
	cases := []struct {
		name    string
		tab     config.Tab
		wantCmd string
		wantPro string
	}{
		{"claude", config.Tab{Cmd: "claude", Prompt: "create mything"}, "claude 'create mything'", ""},
		{"claude with flags", config.Tab{Cmd: "claude --model opus", Prompt: "go"}, "claude --model opus 'go'", ""},
		{"codex", config.Tab{Cmd: "codex", Prompt: "go"}, "codex 'go'", ""},
		{"quotes escaped", config.Tab{Cmd: "claude", Prompt: "it's fine"}, `claude 'it'\''s fine'`, ""},
		{"non-agent untouched", config.Tab{Cmd: "nvim .", Prompt: "typed"}, "nvim .", "typed"},
		{"no prompt", config.Tab{Cmd: "claude"}, "claude", ""},
		{"no cmd", config.Tab{Prompt: "typed"}, "", "typed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inlinePrompt(tc.tab)
			if got.Cmd != tc.wantCmd {
				t.Errorf("cmd: got %q, want %q", got.Cmd, tc.wantCmd)
			}
			if got.Prompt != tc.wantPro {
				t.Errorf("prompt: got %q, want %q", got.Prompt, tc.wantPro)
			}
		})
	}
}

func TestAgentTab(t *testing.T) {
	cases := []struct {
		name     string
		agent    string
		wantName string
		wantCmd  string
		wantSkip bool
	}{
		{"default", "", "claude", "claude --permission-mode auto", false},
		{"claude", "claude", "claude", "claude --permission-mode auto", false},
		{"codex", "codex", "codex", "codex", false},
		{"verbatim command", "claude --model opus", "claude", "claude --model opus", false},
		{"none", "none", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := agentTab(tc.agent, "do work")
			if ok == tc.wantSkip {
				t.Fatalf("ok: got %v, want %v", ok, !tc.wantSkip)
			}
			if tc.wantSkip {
				return
			}
			if got.Name != tc.wantName {
				t.Errorf("name: got %q, want %q", got.Name, tc.wantName)
			}
			if got.Cmd != tc.wantCmd {
				t.Errorf("cmd: got %q, want %q", got.Cmd, tc.wantCmd)
			}
			if got.Prompt != "do work" {
				t.Errorf("prompt: got %q, want %q", got.Prompt, "do work")
			}
		})
	}
}
