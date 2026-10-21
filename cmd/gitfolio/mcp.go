package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
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
			"lines, first and last commit. Also the sync mode and the last sync. Reads this computer only.",
		run: mcpStatus,
	},
	{
		Name:  "devtype",
		Title: "Developer type",
		Description: "The user's developer type from aline.team: the type, its description and hashtags, the main position, " +
			"five scores from 0 to 100 (agility, stability, contribution, adaptability, consistency) for the work made " +
			"without AI and with it, and the type with AI. The saved one unless refresh is true. In local mode the " +
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
		Name:  "portfolio",
		Title: "Portfolio",
		Description: "The user's portfolio from their git history in Markdown, as gitfolio portfolio writes it: developer " +
			"type, tech stack (hidden ones left out) and repositories, the latest work first. Nothing is written to a file.",
		run: mcpPortfolio,
	},
}

func init() {
	for i := range mcpTools {
		mcpTools[i].Annotations = map[string]any{"readOnlyHint": true, "openWorldHint": mcpTools[i].Name != "status"}
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
		WaitingFor   string         `json:"waitingForVerifiedEmail,omitempty"`
	}
	day := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Local().Format(time.DateOnly)
	}
	var list []repo
	for _, r := range repos {
		s := statusOf(r, commits, nil, st)
		list = append(list, repo{r.Name, providerNames[r.Provider], r.Namespace, s.commits, s.sentCount(r, st), s.changes,
			s.add, s.del, s.created, s.aiShare(), s.lines, day(s.first), day(s.last), st.Waiting[r.Provider+"/"+r.Namespace]})
	}
	mode := "automatic"
	if cfg.AutoSyncOff {
		mode = "local"
	}
	return map[string]any{"repositories": list, "syncMode": mode, "lastSync": st.LastSync, "lastSyncError": st.LastError}, nil
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
	return map[string]any{"year": d.Year, "saved": saved, "fetchedAt": d.FetchedAt, "type": d.Detail}, nil
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
	return portfolioOf(dir, detectLang(os.Getenv))
}
