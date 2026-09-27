package main

import (
	"slices"
	"strings"
)

// Agent signatures found in commit metadata (DESIGN 5.3). Add one line per new agent.
var agentEmails = map[string]string{
	"noreply@anthropic.com":  "claude-code",
	"noreply@openai.com":     "codex",
	"codex@openai.com":       "codex",
	"cursoragent@cursor.com": "cursor",
	"copilot@github.com":     "copilot",
	"aider@aider.chat":       "aider",
	"amp@ampcode.com":        "amp",
	"agent@warp.dev":         "warp",
	"oz-agent@warp.dev":      "warp",
}

// githubBots maps the name part of <id>+<name>@users.noreply.github.com (with "[bot]" removed).
var githubBots = map[string]string{
	"copilot":              "copilot",
	"copilot-swe-agent":    "copilot",
	"cursor":               "cursor",
	"devin-ai-integration": "devin",
	"google-labs-jules":    "jules",
	"kiro-agent":           "kiro",
	"opencode-agent":       "opencode",
	"amazon-q-developer":   "amazon-q",
}

var agentMarkers = map[string]string{
	"generated with [claude code]": "claude-code",
	"generated with [codex]":       "codex",
	"made-with: cursor":            "cursor",
	"amp-thread-id:":               "amp",
}

// assistedNames maps words in an "Assisted-by:" trailer to agent ids.
var assistedNames = map[string]string{
	"claude": "claude-code", "codex": "codex", "copilot": "copilot", "cursor": "cursor",
	"gemini": "gemini", "aider": "aider", "devin": "devin", "jules": "jules", "kiro": "kiro",
}

func agentByEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if a, ok := agentEmails[email]; ok {
		return a
	}
	if local, ok := strings.CutSuffix(email, "@users.noreply.github.com"); ok {
		if _, name, found := strings.Cut(local, "+"); found {
			local = name
		}
		return githubBots[strings.TrimSuffix(local, "[bot]")]
	}
	return ""
}

// detectAgents returns the AI agents that the commit's author, trailers or message markers point to.
// It must run on the raw message, before masking hides trailer emails.
func detectAgents(authorName, authorEmail, message string) []string {
	var agents []string
	add := func(a string) {
		for _, x := range agents {
			if x == a {
				return
			}
		}
		if a != "" {
			agents = append(agents, a)
		}
	}
	add(agentByEmail(authorEmail))
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(authorName)), "(aider)") {
		add("aider")
	}
	lower := strings.ToLower(message)
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "co-authored-by:"); ok {
			if i, j := strings.LastIndex(v, "<"), strings.LastIndex(v, ">"); i >= 0 && j > i {
				add(agentByEmail(v[i+1 : j]))
			}
		}
		if v, ok := strings.CutPrefix(line, "assisted-by:"); ok {
			for word, a := range assistedNames {
				if strings.Contains(v, word) {
					add(a)
				}
			}
		}
	}
	for marker, a := range agentMarkers {
		if strings.Contains(lower, marker) {
			add(a)
		}
	}
	slices.Sort(agents) // map iteration order is random; keep output stable
	return agents
}
