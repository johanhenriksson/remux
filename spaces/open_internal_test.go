package spaces

import (
	"testing"
)

func TestAgentName(t *testing.T) {
	cases := []struct {
		branch, space, want string
	}{
		{"johanh/ait-2679-quick-search-based-company-enrichment", "funnel-x", "ait-2679"},
		{"AIT-12-fix", "", "ait-12"},
		{"fix-login-2", "", "fix-login-2"},
		{"feat/my-feature", "", "feat-my-feature"},
		{"herdr-backend", "", "herdr-backend"},
		{"2024-cleanup", "", "agent-2024-cleanup"},
		{"a-very-long-branch-name-that-goes-on-and-on-forever", "", "a-very-long-branch-name-that-goe"},
		{"", "remux-herdr", "remux-herdr"},
	}
	for _, tc := range cases {
		if got := AgentName(tc.branch, tc.space); got != tc.want {
			t.Errorf("AgentName(%q, %q) = %q, want %q", tc.branch, tc.space, got, tc.want)
		}
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
