package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// cmdMCP serves GitFolio to AI coding tools (Claude Code, Codex and others) as an MCP server: JSON-RPC
// 2.0 messages, one per line, on stdin and stdout. Its tools only read: what is collected on this
// computer, and the developer type and tech stack from aline.team. Anything said for people goes to
// stderr, never into the protocol.
func cmdMCP(dir string, args []string) error {
	if len(args) > 0 {
		return failure("usage", "gitfolio mcp")
	}
	out = errOut
	return serveMCP(dir, os.Stdin, os.Stdout)
}

// mcpVersions are the protocol versions GitFolio speaks, the newest first; a client asking for one of
// them gets it, any other the newest.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
	run         func(dir string, args map[string]any) (any, error)
}

var mcpTools = []mcpTool{
	{
		Name:  "status",
		Title: "Collected repositories",
		Description: "The repositories GitFolio collects on this computer and what it holds for each: the user's commits, " +
			"how many of them aline.team has, lines changed, files created, the share made with AI, languages by changed " +
			"lines, first and last commit. Also whether GitFolio is logged in (if not, start it with policy and login_start), " +
			"the sync mode and the last sync. Reads this computer only.",
		run: mcpStatus,
	},
	{
		Name:  "devtype",
		Title: "Developer type",
		Description: "The user's developer type from aline.team: the type, its description and hashtags, the main position, " +
			"five scores from 0 to 100 (agility, stability, contribution, adaptability, consistency) for the work made " +
			"without AI and with it, the type with AI, and aline.team's reasoning for each score in the screen's language. " +
			"The saved type unless refresh is true. In local mode the " +
			"statistics are sent to aline.team first, as gitfolio stats does.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"refresh": map[string]any{"type": "boolean", "description": "Ask aline.team again instead of using the saved one."},
		}},
		run: mcpDevType,
	},
	{
		Name:  "stack",
		Title: "Tech stack periods",
		Description: "How long the user used each language, framework and library, from the first to the last commit of " +
			"the repositories using it, as aline.team works it out. Ones marked hidden are hidden on the user's " +
			"aline.team profile: leave them out of anything shared.",
		run: mcpStack,
	},
	{
		Name:  "work",
		Title: "Work by repository",
		Description: "What the user's work in each repository was made of, counted on this computer, for a period if given: " +
			"active period and months, commits, commit types (feature, fix, refactor, test, docs, ...) and kinds of work " +
			"(source, test, docs, build, ci_deploy, database, config) as percentages, languages, lines, files created, " +
			"the share made with AI, and branch names where the user lets them show. No paths, file or folder names. " +
			"Use it as the facts of a portfolio; ask the user what the work was about.",
		InputSchema: periodSchema(true),
		run:         mcpWork,
	},
	{
		Name:  "timeline",
		Title: "Career timeline",
		Description: "Month by month, for a period if given: the user's commits, the repositories worked in, languages by " +
			"changed lines and commit types. Counted on this computer, for a tech stack career or a timeline of their work.",
		InputSchema: periodSchema(true),
		run:         mcpTimeline,
	},
	{
		Name:  "commits",
		Title: "Commits of a repository",
		Description: "The user's commits in one repository, newest first, for a period and commit type if given: date, type, " +
			"kinds of work, lines, whether made with AI, and the commit message subject (and body where the user allows " +
			"the full messages). Only for repositories whose messages the user lets AI tools see (ai_settings); never " +
			"hashes, paths or file names, and branch names only where shown. Messages are already masked by the user's " +
			"blocked words. Use it to name what the user achieved; take 100 at a time.",
		InputSchema: func() map[string]any {
			s := periodSchema(true)
			props := s["properties"].(map[string]any)
			props["repository"] = map[string]any{"type": "string", "description": "The repository, as status names it."}
			props["type"] = map[string]any{"type": "string", "description": "Only this commit type: feature, fix, refactor, performance, test, docs, chore, ci, hotfix, revert."}
			props["limit"] = map[string]any{"type": "integer", "description": "How many, 1 to 500. Default 100."}
			props["offset"] = map[string]any{"type": "integer", "description": "How many to skip: nextOffset of the last answer."}
			s["required"] = []string{"repository"}
			return s
		}(),
		run: mcpCommits,
	},
	{
		Name:  "ai_settings",
		Title: "What AI tools see",
		Description: "How each repository appears to AI tools, as the user set it: by its real name, anonymous or another " +
			"name, and whether its commit messages (off, subjects, full) and branch names are shown. Hidden repositories " +
			"are only counted. Set these with set_ai_exposure when the user or their codebase's rules say so.",
		run: mcpAISettings,
	},
	{
		Name:  "set_ai_exposure",
		Title: "Set what AI tools see",
		Description: "Change what AI tools may see of one repository through GitFolio. Do it only as the user asks or as the " +
			"rules of their codebase say (for example a CLAUDE.md or AGENTS.md that keeps company names or commit messages " +
			"private). The repository is named as ai_settings shows it, or by a path in it. Paths, folders and file names " +
			"are never shown, whatever is set.",
		InputSchema: map[string]any{"type": "object", "required": []string{"repository"}, "properties": map[string]any{
			"repository": map[string]any{"type": "string", "description": "The repository as ai_settings names it, or a path in it."},
			"name":       map[string]any{"type": "string", "description": "real, anonymous, or another name to show instead."},
			"messages":   map[string]any{"type": "string", "enum": []string{"off", "subjects", "full"}, "description": "What of the commit messages to show."},
			"branches":   map[string]any{"type": "boolean", "description": "Show branch names."},
			"hidden":     map[string]any{"type": "boolean", "description": "Hide the repository from AI tools altogether."},
		}},
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		run:         mcpSetExposure,
	},
	{
		Name:  "portfolio",
		Title: "Portfolio",
		Description: "The user's portfolio from their git history in Markdown, as gitfolio portfolio writes it: developer " +
			"type, tech stack (hidden ones left out) and repositories, the latest work first. Nothing is written to a file.",
		run: mcpPortfolio,
	},
}

// periodSchema is the input of a tool over a period of the history, of one repository if it takes one.
func periodSchema(repository bool) map[string]any {
	props := map[string]any{
		"from": map[string]any{"type": "string", "description": "First year, month or day: 2025, 2025-06 or 2025-06-15. Empty: from the start."},
		"to":   map[string]any{"type": "string", "description": "Last year, month or day, included. Empty: until now."},
	}
	if repository {
		props["repository"] = map[string]any{"type": "string", "description": "One repository, as status names it. Empty: all."}
	}
	return map[string]any{"type": "object", "properties": props}
}

var mcpSetupTools = []mcpTool{
	{
		Name:  "policy",
		Title: "Data policy",
		Description: "GitFolio's data policy and aline.team's terms of service and privacy policy. Before logging the user in, " +
			"show them these and get their agreement: an email without an account is signed up, which means agreeing to them.",
		run: mcpPolicy,
	},
	{
		Name:  "login_start",
		Title: "Log in: send a code",
		Description: "Start GitFolio for the user: sends a one-time code to their email; an email without an aline.team " +
			"account is signed up. The email is the user's own and required: ask them for it, never guess it. Call only " +
			"after showing the policy and getting the user's agreement (agreed: true). Then ask the user for the code and " +
			"call login_verify.",
		InputSchema: map[string]any{"type": "object", "required": []string{"email", "agreed"}, "properties": map[string]any{
			"email":         map[string]any{"type": "string", "description": "The user's email, as they gave it."},
			"agreed":        map[string]any{"type": "boolean", "description": "The user agreed to the data policy, terms and privacy policy."},
			"notifications": map[string]any{"type": "boolean", "description": "A new account: the user wants aline.team service emails. Only if they said yes."},
		}},
		Annotations: mcpWrites,
		run:         mcpLoginStart,
	},
	{
		Name:        "login_verify",
		Title:       "Log in: check the code",
		Description: "Finish logging in with the code the user got by email (login_start). A wrong code can be tried again a few times.",
		InputSchema: map[string]any{"type": "object", "required": []string{"code"}, "properties": map[string]any{
			"code": map[string]any{"type": "string", "description": "The digits from the email, as the user typed them."},
		}},
		Annotations: mcpWrites,
		run:         mcpLoginVerify,
	},
	{
		Name:  "email_start",
		Title: "Add a work email",
		Description: "Add another email the user commits with (a repository's git user.email that is not their account email) " +
			"and send it a code; aline.team takes a repository's commits only under verified emails. Then ask the user " +
			"for the code and call email_verify.",
		InputSchema: map[string]any{"type": "object", "required": []string{"email"}, "properties": map[string]any{
			"email": map[string]any{"type": "string", "description": "The work email, as the user gave it."},
		}},
		Annotations: mcpWrites,
		run:         mcpEmailStart,
	},
	{
		Name:        "email_verify",
		Title:       "Verify a work email",
		Description: "Verify the work email of email_start with the code the user got.",
		InputSchema: map[string]any{"type": "object", "required": []string{"code"}, "properties": map[string]any{
			"code": map[string]any{"type": "string", "description": "The digits from the email, as the user typed them."},
		}},
		Annotations: mcpWrites,
		run:         mcpEmailVerify,
	},
	{
		Name:  "add_repository",
		Title: "Register a repository",
		Description: "Register a git repository on this computer with GitFolio: collects the user's own pushed commits and " +
			"installs git hooks so later pushes are collected. Only repositories the user chose. Afterwards agree with " +
			"them how it appears to AI tools (set_ai_exposure).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "A path in the repository. Empty: the current folder."},
		}},
		Annotations: mcpWrites,
		run:         mcpAddRepository,
	},
	{
		Name:  "set_sync_mode",
		Title: "Choose the sync mode",
		Description: "automatic: the user's commits go to aline.team right after each git push. local: nothing is sent by " +
			"itself; daily statistics go only when GitFolio's devtype, stack or portfolio is asked for. Ask the user.",
		InputSchema: map[string]any{"type": "object", "required": []string{"mode"}, "properties": map[string]any{
			"mode": map[string]any{"type": "string", "enum": []string{"automatic", "local"}},
		}},
		Annotations: mcpWrites,
		run:         mcpSetSyncMode,
	},
}

// mcpWrites marks a tool that changes something here or on aline.team, nothing it cannot undo.
var mcpWrites = map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": true}

func init() {
	mcpTools = append(mcpSetupTools, mcpTools...)
	for i := range mcpTools {
		if mcpTools[i].Annotations == nil {
			local := slices.Contains([]string{"status", "work", "timeline", "commits", "ai_settings", "policy"}, mcpTools[i].Name) // this computer only
			mcpTools[i].Annotations = map[string]any{"readOnlyHint": true, "openWorldHint": !local}
		}
		if mcpTools[i].InputSchema == nil {
			mcpTools[i].InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
	}
}

func serveMCP(dir string, r io.Reader, w io.Writer) error {
	dec, enc := json.NewDecoder(r), json.NewEncoder(w)
	reply := func(id json.RawMessage, result any, code int, msg string) error {
		m := map[string]any{"jsonrpc": "2.0", "id": id}
		if code != 0 {
			m["error"] = map[string]any{"code": code, "message": msg}
		} else {
			m["result"] = result
		}
		return enc.Encode(m)
	}
	for {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil { // the stream cannot be read past this
			return reply(json.RawMessage("null"), nil, -32700, "parse error: "+err.Error())
		}
		if len(req.ID) == 0 { // a notification: nothing is answered
			continue
		}
		var err error
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			v := mcpVersions[0]
			if slices.Contains(mcpVersions, p.ProtocolVersion) {
				v = p.ProtocolVersion
			}
			err = reply(req.ID, map[string]any{
				"protocolVersion": v,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "gitfolio", "title": "GitFolio", "version": version},
				"instructions": "GitFolio collects the metadata of the user's own pushed commits (no source code) and aline.team " +
					"works out a developer type and tech stack from it. Everything here is the user's own; tech marked hidden " +
					"is hidden on their profile, so leave it out of anything shared.",
			}, 0, "")
		case "ping":
			err = reply(req.ID, map[string]any{}, 0, "")
		case "tools/list":
			err = reply(req.ID, map[string]any{"tools": mcpTools}, 0, "")
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &p) != nil {
				err = reply(req.ID, nil, -32602, "invalid params")
				break
			}
			i := slices.IndexFunc(mcpTools, func(t mcpTool) bool { return t.Name == p.Name })
			if i < 0 {
				err = reply(req.ID, nil, -32602, "unknown tool: "+p.Name)
				break
			}
			err = reply(req.ID, mcpResult(mcpTools[i].run(dir, p.Arguments)), 0, "")
		default:
			err = reply(req.ID, nil, -32601, "method not found: "+req.Method)
		}
		if err != nil {
			return err
		}
	}
}

// mcpResult is a tool's answer: its value as JSON text (Markdown as is), or the error the user would see.
func mcpResult(v any, err error) map[string]any {
	if err != nil {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}
	}
	text, ok := v.(string)
	if !ok {
		b, err := json.Marshal(v)
		if err != nil {
			return mcpResult(nil, err)
		}
		text = string(b)
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": false}
}

func mcpStatus(dir string, _ map[string]any) (any, error) {
	var repos []Repo
	var commits []Commit
	var st syncState
	var cfg Config
	if err := withLock(dir, func() error { // a push may be writing them
		var err error
		if repos, err = loadRepos(dir); err != nil {
			return err
		}
		if commits, err = readCommits(dir); err != nil {
			return err
		}
		if st, err = loadSync(dir); err != nil {
			return err
		}
		cfg, err = loadConfig(dir)
		return err
	}); err != nil {
		return nil, err
	}
	type repo struct {
		Name         string         `json:"name"`
		Service      string         `json:"service,omitempty"`
		Namespace    string         `json:"repository,omitempty"` // owner/repo on the service, never a path here
		Commits      int            `json:"commits"`
		Sent         string         `json:"onAlineTeam"`
		FileChanges  int            `json:"fileChanges"`
		LinesAdded   int            `json:"linesAdded"`
		LinesDeleted int            `json:"linesDeleted"`
		FilesCreated int            `json:"filesCreated"`
		WithAI       string         `json:"withAI"`
		Languages    map[string]int `json:"changedLinesByLanguage"`
		FirstCommit  string         `json:"firstCommit,omitempty"`
		LastCommit   string         `json:"lastCommit,omitempty"`
		Waiting      bool           `json:"waitingForVerifiedEmail,omitempty"` // which email: gitfolio email
	}
	day := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Local().Format(time.DateOnly)
	}
	seen := map[string]Repo{}
	for _, v := range aiView(detectLang(os.Getenv), repos) {
		seen[v.ID] = v
	}
	var list []repo
	for _, r := range repos {
		v, ok := seen[r.ID]
		if !ok {
			continue
		}
		s := statusOf(r, commits, nil, st)
		list = append(list, repo{v.Name, providerNames[r.Provider], v.Namespace, s.commits, s.sentCount(r, st), s.changes,
			s.add, s.del, s.created, s.aiShare(), s.lines, day(s.first), day(s.last), st.Waiting[r.Provider+"/"+r.Namespace] != ""})
	}
	mode := "automatic"
	if cfg.AutoSyncOff {
		mode = "local"
	}
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	return map[string]any{"loggedIn": c.loggedIn(), "repositories": list, "syncMode": mode, "lastSync": st.LastSync,
		"lastSyncError": st.LastError}, nil
}

func mcpDevType(dir string, args map[string]any) (any, error) {
	lang := detectLang(os.Getenv)
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	local, err := sendIfLocal(dir, lang, c)
	if err != nil {
		return nil, err
	}
	refresh, _ := args["refresh"].(bool)
	d, saved, err := devTypeNow(dir, lang, c, refresh || local)
	if err != nil {
		return nil, err
	}
	if d.Detail.Title == "" {
		return nil, errors.New(strings.TrimSpace(tr(lang, "statsEmpty")))
	}
	res := map[string]any{"year": d.Year, "saved": saved, "fetchedAt": d.FetchedAt, "type": d.Detail}
	// Why each score is what it is, and the type with AI described, asked for this answer only:
	// devtype.json keeps just what gitfolio stats shows.
	var raw map[string]any
	if c.call("GET", fmt.Sprintf("/cli/devtype?yearPeriod=%d", d.Year), nil, &raw) == nil {
		why := map[string]string{}
		for k, v := range raw {
			s, _ := v.(string)
			if s != "" && (strings.Contains(k, "Reasoning") || k == "devTypeDescriptionAi" || k == "devTypeHashTagsAi") {
				why[k] = s
			}
		}
		if len(why) > 0 {
			res["reasoning"] = why
		}
	}
	return res, nil
}

func mcpStack(dir string, _ map[string]any) (any, error) {
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	lang := detectLang(os.Getenv)
	if _, err := sendIfLocal(dir, lang, c); err != nil {
		return nil, err
	}
	u, err := techUsagesNow(c)
	if unsupported(err) {
		return nil, errors.New(strings.TrimSpace(tr(lang, "stackUnavailable")))
	}
	return u, err
}

func mcpPortfolio(dir string, _ map[string]any) (any, error) {
	return portfolioOf(dir, detectLang(os.Getenv), true)
}

// aiView is the registered repositories as AI tools see them (config ai): hidden ones left out, the
// others under the name the user chose, without their path here, and without their address on the
// git service unless they go by their own name.
func aiView(lang string, repos []Repo) []Repo {
	var view []Repo
	n := 0
	for _, r := range repos {
		if r.AI.Hidden {
			continue
		}
		switch r.AI.Name {
		case "":
		case anonymousName:
			n++
			r.Name, r.Namespace = fmt.Sprintf(tr(lang, "aiAnonymous"), n), ""
		default:
			r.Name, r.Namespace = r.AI.Name, ""
		}
		r.Path = ""
		view = append(view, r)
	}
	return view
}

// aiRepo finds the repository an AI tool means: by the name it sees, its own name or a path in it.
func aiRepo(lang string, repos []Repo, arg string) (int, bool) {
	for _, v := range aiView(lang, repos) {
		if v.Name == arg {
			return slices.IndexFunc(repos, func(r Repo) bool { return r.ID == v.ID }), true
		}
	}
	r, ok := findRepo(repos, arg)
	return slices.IndexFunc(repos, func(x Repo) bool { return x.ID == r.ID }), ok
}

// setExposure changes one setting of what AI tools see of a repository: name (real, anonymous or
// another name), messages (off, subjects, full), branches (on, off), or hidden (true, false).
func setExposure(e *aiExposure, key, value string) bool {
	switch key {
	case "name":
		switch value = strings.TrimSpace(value); value {
		case "", anonymousName:
			return false
		case "real":
			e.Name = ""
		case "anonymous":
			e.Name = anonymousName
		default:
			e.Name = value
		}
	case "messages":
		if !slices.Contains([]string{"off", "subjects", "full"}, value) {
			return false
		}
		e.Messages = strings.TrimPrefix(value, "off")
	case "branches", "hidden":
		on := value == "on" || value == "true"
		if !on && value != "off" && value != "false" {
			return false
		}
		if key == "branches" {
			e.Branches = on
		} else {
			e.Hidden = on
		}
	default:
		return false
	}
	return true
}

// exposureText is how a repository's settings read: "as itself, messages off, branches off".
func exposureText(lang string, r Repo, seenAs string) string {
	if r.AI.Hidden {
		return tr(lang, "aiHiddenWord")
	}
	return fmt.Sprintf(tr(lang, "aiExposure"), seenAs, cmp.Or(r.AI.Messages, "off"), map[bool]string{true: "on", false: "off"}[r.AI.Branches])
}

const configAIUsage = "gitfolio config ai [name <repo> real|anonymous|<name> | messages <repo> off|subjects|full | branches <repo> on|off | hide <repo> | show <repo>]"

// cmdConfigAI shows or changes what AI coding tools see of each repository through gitfolio mcp.
// The repository is named as gitfolio status names it, or by a path in it.
func cmdConfigAI(dir string, args []string) error {
	lang := detectLang(os.Getenv)
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	seen := map[string]string{}
	for _, v := range aiView(lang, repos) {
		seen[v.ID] = v.Name
	}
	if len(args) == 0 {
		var b strings.Builder
		for _, r := range repos {
			fmt.Fprintf(&b, "%s: %s\n", r.Name, exposureText(lang, r, seen[r.ID]))
		}
		sayKV(lang, "aiTitle", b.String())
		return nil
	}
	words := []string{"name", "messages", "branches", "hide", "show"}
	if !slices.Contains(words, args[0]) || len(args) < 2 {
		return wrongWord("config ai", args[0], words, configAIUsage)
	}
	r, ok := findRepo(repos, args[1])
	if !ok {
		return failure("notRegistered", args[1])
	}
	i := slices.IndexFunc(repos, func(x Repo) bool { return x.ID == r.ID })
	key, value := args[0], strings.Join(args[2:], " ")
	switch key {
	case "hide", "show":
		key, value = "hidden", map[bool]string{true: "true", false: "false"}[key == "hide"]
	}
	if (key != "hidden" && len(args) < 3) || !setExposure(&repos[i].AI, key, value) {
		return failure("usage", configAIUsage)
	}
	if err := saveRepos(dir, repos); err != nil {
		return err
	}
	for _, v := range aiView(lang, repos) {
		seen[v.ID] = v.Name
	}
	say(lang, "aiSet", repos[i].Name, exposureText(lang, repos[i], seen[repos[i].ID]))
	return nil
}

func mcpAISettings(dir string, _ map[string]any) (any, error) {
	lang := detectLang(os.Getenv)
	repos, err := loadRepos(dir)
	if err != nil {
		return nil, err
	}
	type setting struct {
		Repository string `json:"repository"`
		Name       string `json:"name"` // real, anonymous or alias
		Messages   string `json:"messages"`
		Branches   string `json:"branches"`
	}
	view, list := aiView(lang, repos), []setting{}
	for _, v := range view {
		r := repos[slices.IndexFunc(repos, func(x Repo) bool { return x.ID == v.ID })]
		name := map[string]string{"": "real", anonymousName: "anonymous"}[r.AI.Name]
		list = append(list, setting{v.Name, cmp.Or(name, "alias"), cmp.Or(r.AI.Messages, "off"),
			map[bool]string{true: "on", false: "off"}[r.AI.Branches]})
	}
	return map[string]any{"repositories": list, "hidden": len(repos) - len(view)}, nil
}

func mcpSetExposure(dir string, args map[string]any) (any, error) {
	lang := detectLang(os.Getenv)
	arg, _ := args["repository"].(string)
	var changed Repo
	err := withLock(dir, func() error {
		repos, err := loadRepos(dir)
		if err != nil {
			return err
		}
		i, ok := aiRepo(lang, repos, arg)
		if !ok || i < 0 {
			return failure("notRegistered", arg)
		}
		for _, key := range []string{"name", "messages", "branches", "hidden"} {
			v, ok := args[key]
			if !ok {
				continue
			}
			if !setExposure(&repos[i].AI, key, fmt.Sprint(v)) {
				return errors.New("invalid " + key + ": " + fmt.Sprint(v))
			}
		}
		changed = repos[i]
		return saveRepos(dir, repos)
	})
	if err != nil {
		return nil, err
	}
	if changed.AI.Hidden {
		return "Hidden from AI tools.", nil
	}
	return mcpAISettings(dir, nil)
}

// mcpPending is a code sent by email that a later tool call answers: a sign-in or a work email check.
var mcpPending struct {
	login, email *codeStart
	address      string
	tries        int
}

func mcpPolicy(_ string, _ map[string]any) (any, error) {
	return map[string]any{
		"dataPolicy":     strings.TrimSpace(tr(detectLang(os.Getenv), "policy")),
		"termsOfService": "https://aline.team/terms",
		"privacyPolicy":  "https://aline.team/privacy",
		"signingUp":      "An email without an aline.team account is signed up, which means agreeing to the terms of service and the privacy policy.",
		"aiTools":        "What GitFolio's tools return becomes part of this conversation; gitfolio config ai (set_ai_exposure) sets what they show.",
	}, nil
}

func mcpLoginStart(dir string, args map[string]any) (any, error) {
	email, _ := args["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	if agreed, _ := args["agreed"].(bool); !agreed {
		return nil, errors.New("show the user GitFolio's data policy and aline.team's terms (the policy tool), and call again with agreed: true once they agree")
	}
	if !validEmail(email) {
		return nil, errors.New("not an email address: " + email)
	}
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	if c.loggedIn() {
		if strings.EqualFold(c.creds.Email, email) {
			return map[string]any{"loggedIn": true, "next": "Already logged in with this email."}, nil
		}
		return nil, errors.New("this computer is logged in with another email; the user can run gitfolio logout in a terminal first")
	}
	s, err := c.startSignIn(email)
	if err != nil {
		return nil, err
	}
	if !s.exists {
		notify, _ := args["notifications"].(bool)
		s.req["isNotified"] = notify // service emails: only when the user said yes
	}
	mcpPending.login, mcpPending.tries = &s, 0
	return map[string]any{"codeSentTo": email, "newAccount": !s.exists, "codeDigits": s.length,
		"next": "Ask the user for the code in that email, then call login_verify with it."}, nil
}

func mcpLoginVerify(dir string, args map[string]any) (any, error) {
	s := mcpPending.login
	if s == nil {
		return nil, errors.New("no code is waiting: call login_start first")
	}
	code, _ := args["code"].(string)
	if code = strings.TrimSpace(code); !validCode(code, s.length) {
		return nil, fmt.Errorf("the code is %d digits", s.length)
	}
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	res, err := c.finishSignIn(*s, code)
	if err != nil {
		return nil, mcpCodeError(err, func() { mcpPending.login = nil })
	}
	mcpPending.login = nil
	r := map[string]any{"loggedIn": true, "newAccount": res.Account.Created,
		"next": "Register the user's repositories with add_repository, choose the sync mode with set_sync_mode, and agree with " +
			"the user what AI tools may see of each repository (set_ai_exposure)."}
	if res.Account.Created {
		r["webLogin"] = strings.TrimSpace(tr(detectLang(os.Getenv), "passwordMail"))
	}
	return r, nil
}

// mcpCodeError is a wrong code's answer while tries are left; otherwise the code waiting is dropped.
func mcpCodeError(err error, drop func()) error {
	if wrongCode(err) {
		if mcpPending.tries++; mcpPending.tries < maxCodeTries {
			return fmt.Errorf("wrong code: ask the user again (%d tries left)", maxCodeTries-mcpPending.tries)
		}
		err = errors.New("too many wrong codes: start again")
	}
	drop()
	return err
}

func mcpEmailStart(dir string, args map[string]any) (any, error) {
	email, _ := args["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	if !c.loggedIn() {
		return nil, errors.New("not logged in: call login_start first")
	}
	if err := withLock(dir, func() error {
		cfg, err := loadConfig(dir)
		if err != nil {
			return err
		}
		if bad := addEmails(&cfg, []string{email}); len(bad) > 0 {
			return errors.New("not an email address: " + email)
		}
		return saveConfig(dir, cfg)
	}); err != nil {
		return nil, err
	}
	s, done, err := c.startEmailCheck(email)
	if err != nil {
		return nil, err
	}
	if done {
		return map[string]any{"verified": true}, nil
	}
	mcpPending.email, mcpPending.address, mcpPending.tries = &s, email, 0
	return map[string]any{"codeSentTo": email, "codeDigits": s.length,
		"next": "Ask the user for the code in that email, then call email_verify with it."}, nil
}

func mcpEmailVerify(dir string, args map[string]any) (any, error) {
	s := mcpPending.email
	if s == nil {
		return nil, errors.New("no code is waiting: call email_start first")
	}
	code, _ := args["code"].(string)
	if code = strings.TrimSpace(code); !validCode(code, s.length) {
		return nil, fmt.Errorf("the code is %d digits", s.length)
	}
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	if err := c.finishEmailCheck(*s, code); err != nil {
		return nil, mcpCodeError(err, func() { mcpPending.email = nil })
	}
	mcpPending.email = nil
	return map[string]any{"verified": true, "email": mcpPending.address}, nil
}

func mcpAddRepository(dir string, args map[string]any) (any, error) {
	p, _ := args["path"].(string)
	c, err := newClient(dir)
	if err != nil {
		return nil, err
	}
	if !c.loggedIn() {
		return nil, errors.New("not logged in: call login_start first")
	}
	top, err := topLevel(cmp.Or(p, "."))
	if err != nil {
		return nil, err
	}
	var n int
	if err := withLock(dir, func() error { n, err = registerRepo(dir, newRepo(top), true); return err }); err != nil {
		return nil, err
	}
	return map[string]any{"repository": filepath.Base(top), "commitsCollected": n,
		"next": "Agree with the user how this repository appears to AI tools (set_ai_exposure): by its real name (default), " +
			"anonymous or another name; its commit messages and branch names stay hidden unless they allow them. " +
			"Follow the rules of the codebase if it has any about company names or commit messages."}, nil
}

func mcpSetSyncMode(dir string, args map[string]any) (any, error) {
	mode, _ := args["mode"].(string)
	if mode != "automatic" && mode != "local" {
		return nil, errors.New("mode is automatic or local")
	}
	lang := detectLang(os.Getenv)
	if err := withLock(dir, func() error {
		cfg, err := loadConfig(dir)
		if err != nil {
			return err
		}
		cfg.AutoSyncOff, cfg.LocalModeKnown = mode == "local", true
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		if cfg.AutoSyncOff && cfg.Schedule != "" { // local mode sends nothing by itself: no daily sync either
			return setSchedule(dir, lang, "")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]any{"syncMode": mode}, nil
}
