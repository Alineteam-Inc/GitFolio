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
	for _, e := range coAuthorEmails(message) {
		add(agentByEmail(e))
	}
	lower := strings.ToLower(message)
	for _, line := range strings.Split(lower, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "assisted-by:"); ok {
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

// agentEnv maps environment variables that AI agents set on the commands they run (DESIGN 5.3).
// Git passes them on to hooks, so post-commit sees them even when the commit has no trailer.
var agentEnv = map[string]string{
	"CLAUDECODE":       "claude-code",
	"CODEX_CI":         "codex",
	"CODEX_THREAD_ID":  "codex",
	"CURSOR_AGENT":     "cursor",
	"COPILOT_AGENT":    "copilot",
	"COPILOT_CLI":      "copilot",
	"GEMINI_CLI":       "gemini",
	"OPENCODE":         "opencode",
	"CLINE_ACTIVE":     "cline",
	"ROO_ACTIVE":       "roo-code",
	"AGENT_SESSION_ID": "goose",
}

// agentsFromEnv returns the AI agents whose environment variables are set.
func agentsFromEnv(getenv func(string) string) []string {
	var agents []string
	for k, a := range agentEnv {
		if v := getenv(k); v != "" && v != "0" && v != "false" {
			agents = append(agents, a)
		}
	}
	// Cross-vendor conventions: AI_AGENT ("claude-code_2-1-283_agent", "devin@1") and AGENT ("amp").
	for _, k := range []string{"AI_AGENT", "AGENT"} {
		v := strings.ToLower(strings.TrimSpace(getenv(k)))
		if i := strings.IndexAny(v, "_@"); i >= 0 {
			v = v[:i]
		}
		if v != "" && v != "1" && v != "true" {
			agents = append(agents, v)
		}
	}
	slices.Sort(agents)
	return slices.Compact(agents)
}

// coAuthorEmails returns the lowercased emails of the message's Co-authored-by trailers.
func coAuthorEmails(message string) []string {
	var emails []string
	for _, line := range strings.Split(message, "\n") {
		v, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(line)), "co-authored-by:")
		if !ok {
			continue
		}
		if i, j := strings.LastIndex(v, "<"), strings.LastIndex(v, ">"); i >= 0 && j > i {
			emails = append(emails, strings.TrimSpace(v[i+1:j]))
		}
	}
	return emails
}

// How a collected commit was made (DESIGN 5.1).
const (
	creationHuman     = "HUMAN"       // authored by the user, no AI signal
	creationHumanCoAI = "HUMAN_CO_AI" // authored by the user with an AI agent involved
	creationAICoHuman = "AI_CO_HUMAN" // authored by an AI agent with the user as co-author
)

// creationType tells whether c belongs to the user and how it was made; "" means it is not the user's.
// An agent's commit without the user as co-author cannot be attributed to anyone, so it is skipped.
func creationType(c Commit, mine map[string]bool) string {
	if mine[strings.ToLower(c.AuthorEmail)] {
		if len(c.AIAgents) > 0 {
			return creationHumanCoAI
		}
		return creationHuman
	}
	if agentByEmail(c.AuthorEmail) != "" {
		for _, e := range c.coAuthors {
			if mine[e] {
				return creationAICoHuman
			}
		}
	}
	return ""
}
