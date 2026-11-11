package config

import "testing"

func TestInlinePrompt(t *testing.T) {
	cases := []struct {
		name    string
		tab     Tab
		wantCmd string
		wantPro string
	}{
		{"claude", Tab{Cmd: "claude", Prompt: "create mything"}, "claude 'create mything'", ""},
		{"claude with flags", Tab{Cmd: "claude --model opus", Prompt: "go"}, "claude --model opus 'go'", ""},
		{"codex", Tab{Cmd: "codex", Prompt: "go"}, "codex 'go'", ""},
		{"quotes escaped", Tab{Cmd: "claude", Prompt: "it's fine"}, `claude 'it'\''s fine'`, ""},
		{"non-agent untouched", Tab{Cmd: "nvim .", Prompt: "typed"}, "nvim .", "typed"},
		{"no prompt", Tab{Cmd: "claude"}, "claude", ""},
		{"no cmd", Tab{Prompt: "typed"}, "", "typed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.tab.InlinePrompt()
			if got.Cmd != tc.wantCmd {
				t.Errorf("cmd: got %q, want %q", got.Cmd, tc.wantCmd)
			}
			if got.Prompt != tc.wantPro {
				t.Errorf("prompt: got %q, want %q", got.Prompt, tc.wantPro)
			}
		})
	}
}

func TestAgentKind(t *testing.T) {
	kind, args, ok := AgentKind("/usr/local/bin/claude --permission-mode auto")
	if !ok || kind != "claude" || len(args) != 2 || args[1] != "auto" {
		t.Fatalf("got %q %q %v", kind, args, ok)
	}
	if _, _, ok := AgentKind("nvim ."); ok {
		t.Fatal("nvim is not an agent")
	}
}
