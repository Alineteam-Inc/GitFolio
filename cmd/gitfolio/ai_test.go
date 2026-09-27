package main

import (
	"strings"
	"testing"
)

func TestAgentsFromEnv(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{}, ""},
		{map[string]string{"CLAUDECODE": "1", "AI_AGENT": "claude-code_2-1-283_agent"}, "claude-code"},
		{map[string]string{"CODEX_CI": "1"}, "codex"},
		{map[string]string{"AGENT": "amp"}, "amp"},
		{map[string]string{"OPENCODE": "1", "AGENT": "1"}, "opencode"},
		{map[string]string{"ROO_ACTIVE": "false", "CLAUDECODE": ""}, ""},
	} {
		got := strings.Join(agentsFromEnv(func(k string) string { return tc.env[k] }), ",")
		if got != tc.want {
			t.Errorf("agentsFromEnv(%v) = %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestDetectAgents(t *testing.T) {
	for _, tc := range []struct{ name, email, msg, want string }{
		{"me", "me@x.com", "fix bug", ""},
		{"me", "me@x.com", "feat\n\nCo-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>", "claude-code"},
		{"me", "me@x.com", "feat\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)", "claude-code"},
		{"Codex", "codex@openai.com", "feat", "codex"},
		{"me (aider)", "me@x.com", "feat", "aider"},
		{"Copilot", "198982749+Copilot@users.noreply.github.com", "feat", "copilot"},
		{"bot", "opencode-agent[bot]@users.noreply.github.com", "feat", "opencode"},
		{"me", "me@x.com", "feat\n\nCo-authored-by: Dunn <72735688+Dunn-Kim@users.noreply.github.com>", ""},
		{"me", "me@x.com", "feat\n\nAssisted-by: OpenAI Codex", "codex"},
		{"me", "me@x.com", "feat\n\nMade-with: Cursor\nCo-authored-by: Cursor <cursoragent@cursor.com>", "cursor"},
	} {
		if got := strings.Join(detectAgents(tc.name, tc.email, tc.msg), ","); got != tc.want {
			t.Errorf("detectAgents(%q, %q, %q) = %q, want %q", tc.name, tc.email, tc.msg, got, tc.want)
		}
	}
}
