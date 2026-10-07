package main

import (
	"testing"
	"time"
)

// gitfolio status counts one repository's own commits only: what aline.team has of them, file changes and
// lines, AI use, languages by changed lines (data and prose files count for none) and the dependencies found.
func TestStatusOf(t *testing.T) {
	r := Repo{ID: "r1", Name: "app", Provider: "GITHUB", Namespace: "me/app"}
	commits := []Commit{
		{Repo: "r1", Hash: "c1", Date: "2026-10-01T10:00:00+09:00", CreationType: "HUMAN",
			Files: []FileStat{{Name: "cmd/main.go", Add: 10, Del: 2, Created: true}, {Name: "README.md", Add: 5}}},
		{Repo: "r1", Hash: "c2", Date: "2026-10-03T10:00:00+09:00", CreationType: "HUMAN_CO_AI", AIAgents: []string{"claude-code"},
			Files: []FileStat{{Name: "web/A.TSX", Add: 3, Del: 1}}},
		{Repo: "r2", Hash: "x1", Date: "2026-10-05T10:00:00+09:00", Files: []FileStat{{Name: "x.go", Add: 100}}},
	}
	deps := []Dependency{{Repo: "r1", Ecosystem: "npm", Name: "react", Version: "18.2.0"}, {Repo: "r1", Ecosystem: "npm", Name: "next"},
		{Repo: "r2", Ecosystem: "go", Name: "x"}}
	st := syncState{Commits: map[string]string{"GITHUB/me/app/c1": "f"}, Waiting: map[string]string{}}

	s := statusOf(r, commits, deps, st)
	if s.commits != 2 || s.sent != 1 || s.changes != 3 || s.created != 1 || s.add != 18 || s.del != 3 || s.ai != 1 || s.agents["claude-code"] != 1 {
		t.Errorf("status = %+v", s)
	}
	for _, c := range []struct{ got, want string }{
		{s.languageShares(0), "Go 75%, TypeScript 25%"},
		{s.languageShares(1), "Go 75%"},
		{s.depCounts(true), "npm 2"},
		{s.depCounts(false), "off"},
		{s.aiShare(), "50%"},
		{s.sentCount(r, st), "1"},
		{s.sentCount(Repo{}, st), "-"}, // no git service remote: never sent
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	if want := time.Date(2026, 10, 3, 10, 0, 0, 0, time.FixedZone("", 9*3600)); !s.last.Equal(want) {
		t.Errorf("last commit %v", s.last)
	}
	st.Waiting["GITHUB/me/app"] = "me@work.com"
	if got := s.sentCount(r, st); got != "waiting" {
		t.Errorf("waiting repository shows %q", got)
	}
	tiny := repoStatus{lines: map[string]int{"Go": 999, "SQL": 1}}
	if got := tiny.languageShares(0); got != "Go 100%, SQL <1%" {
		t.Errorf("a language under half a percent shows %q", got)
	}
	bin := statusOf(r, []Commit{{Repo: "r1", Files: []FileStat{{Name: "logo.go"}}}}, nil, st) // no lines changed
	if got := bin.languageShares(0); got != "-" {
		t.Errorf("a file with no changed lines counts for %q", got)
	}
	for file, want := range map[string]string{
		"build/Dockerfile": "", "Makefile": "", "go.sum": "", "docs/a.yaml": "", ".bashrc": "", "a.": "",
		"web/App.TSX": "TypeScript", "lib/index.cjs": "JavaScript", "style.scss": "CSS", "deploy.ps1": "PowerShell",
		"rtl/top.sv": "Verilog", "game/player.gd": "GDScript", "conf/app.properties": "Java Properties",
	} {
		if got := languageOf(file); got != want {
			t.Errorf("languageOf(%q) = %q, want %q", file, got, want)
		}
	}
}
