package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"src/a.go":           "a.go",
		"src/{a.go => b.go}": "b.go",
		"{src => lib}/x.go":  "x.go",
		"{ => sub}/x.go":     "x.go",
		"old.go => new.go":   "new.go",
		`"dir/tab\there.go"`: "tab\there.go",
		"docs/한글.md":         "한글.md",
	} {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/Alineteam-Inc/GitFolio.git":        "GITHUB Alineteam-Inc/GitFolio",
		"https://x-access-token:ghp_secret@github.com/o/r.git": "GITHUB o/r",
		"git@github.com:Alineteam-Inc/GitFolio.git":            "GITHUB Alineteam-Inc/GitFolio",
		"ssh://git@GitHub.com:22/o/r.git":                      "GITHUB o/r",
		"git@gitlab.com:group/sub/project.git":                 "GITLAB group/sub/project",
		"https://gitlab.company.internal/group/project/":       "OTHER group/project",
		"/srv/git/repo.git":                                    " ",
		"file:///srv/git/repo.git":                             " ",
		`C:\work\repo`:                                         " ",
	} {
		provider, ns := parseRemote(in)
		if got := provider + " " + ns; got != want {
			t.Errorf("parseRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScanRepo builds a real repository with the user's, someone else's, binary,
// renamed and merge commits, then checks what gets stored and that rescans add nothing.
func TestScanRepo(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, data := t.TempDir(), t.TempDir()

	me := []string{"GIT_AUTHOR_NAME=me", "GIT_AUTHOR_EMAIL=Me@Example.com", "GIT_COMMITTER_NAME=me", "GIT_COMMITTER_EMAIL=me@example.com"}
	other := []string{"GIT_AUTHOR_NAME=o", "GIT_AUTHOR_EMAIL=other@example.com", "GIT_COMMITTER_NAME=o", "GIT_COMMITTER_EMAIL=other@example.com"}
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run(nil, "init", "-q", "-b", "main")
	run(nil, "config", "user.email", "me@example.com")
	write("src/a.go", "1\n2\n3\n")
	write("logo.png", "\x00\x01\x02")
	run(me, "add", ".")
	run(me, "commit", "-q", "-m", "first\n\nbody line")
	write("other.txt", "x\n")
	run(other, "add", ".")
	run(other, "commit", "-q", "-m", "other")
	run(me, "mv", "src/a.go", "src/b.go")
	run(me, "commit", "-q", "-m", "rename")
	run(me, "checkout", "-q", "-b", "side")
	write("c.txt", "x\n")
	run(me, "add", ".")
	run(me, "commit", "-q", "-m", "side\n\nCo-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>")
	run(me, "checkout", "-q", "main")
	run(me, "merge", "-q", "--no-ff", "-m", "merge", "side")
	// An AI agent commits for the user (kept) and for someone else (skipped).
	bot := []string{"GIT_AUTHOR_NAME=Copilot", "GIT_AUTHOR_EMAIL=198982749+Copilot@users.noreply.github.com", "GIT_COMMITTER_NAME=GitHub", "GIT_COMMITTER_EMAIL=noreply@github.com"}
	write("d.txt", "x\n")
	run(bot, "add", ".")
	run(bot, "commit", "-q", "-m", "bot work\n\nCo-authored-by: me <me@example.com>")
	write("e.txt", "x\n")
	run(bot, "add", ".")
	run(bot, "commit", "-q", "-m", "bot for other\n\nCo-authored-by: o <other@example.com>")

	r := newRepo(repo)
	n, err := scanRepo(data, &r, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("first scan added %d commits, want 4 (first, rename, side, bot work)", n)
	}
	if n, err := scanRepo(data, &r, false); err != nil || n != 0 {
		t.Fatalf("rescan added %d commits (err %v), want 0", n, err)
	}
	if n, err := scanRepo(data, &r, true); err != nil || n != 4 {
		t.Fatalf("rebuild added %d commits (err %v), want 4", n, err)
	}

	stored, err := readCommits(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 4 {
		t.Fatalf("stored %d commits after rebuild, want 4", len(stored))
	}
	byMsg := map[string]Commit{}
	for _, c := range stored {
		byMsg[c.Message] = c
		if len(c.Hash) != 40 {
			t.Errorf("%q: hash %q", c.Message, c.Hash)
		}
	}
	// Trailer emails are masked in storage, but AI detection ran on the raw message before that.
	side := "side\n\nCo-Authored-By: Claude Opus 5.5 <[EMAIL]>"
	botWork := "bot work\n\nCo-authored-by: me <[EMAIL]>"
	for msg, want := range map[string]string{
		"first\n\nbody line": "HUMAN",
		"rename":             "HUMAN",
		side:                 "HUMAN_CO_AI",
		botWork:              "AI_CO_HUMAN",
	} {
		if got := byMsg[msg].CreationType; got != want {
			t.Errorf("%q creationType = %q, want %q", msg, got, want)
		}
	}
	if a := byMsg[side].AIAgents; len(a) != 1 || a[0] != "claude-code" {
		t.Errorf("side aiAgents = %v, want [claude-code]", a)
	}
	if a := byMsg[botWork].AIAgents; len(a) != 1 || a[0] != "copilot" {
		t.Errorf("bot work aiAgents = %v, want [copilot]", a)
	}
	if e := byMsg["rename"].AuthorEmail; e != "Me@Example.com" {
		t.Errorf("rename authorEmail = %q", e)
	}
	fs := func(name string, add, del int) FileStat { return FileStat{Name: name, Add: add, Del: del} }
	want := map[string][]FileStat{
		"first\n\nbody line": {fs("logo.png", 0, 0), fs("a.go", 3, 0)}, // numstat lists paths sorted: logo.png < src/a.go
		"rename":             {fs("b.go", 0, 0)},
		side:                 {fs("c.txt", 1, 0)},
		botWork:              {fs("d.txt", 1, 0)},
	}
	for msg, files := range want {
		c, ok := byMsg[msg]
		if !ok {
			t.Errorf("commit %q not stored", msg)
			continue
		}
		if len(c.Files) != len(files) {
			t.Errorf("%q files = %+v, want %+v", msg, c.Files, files)
			continue
		}
		for i := range files {
			if c.Files[i] != files[i] {
				t.Errorf("%q file %d = %+v, want %+v", msg, i, c.Files[i], files[i])
			}
		}
	}

	// A newly blocked word is applied to stored commit messages, and only to them: file names stay.
	if err := cmdConfig(data, []string{"mask", "add", "body", "logo"}); err != nil {
		t.Fatal(err)
	}
	if stored, err = readCommits(data); err != nil {
		t.Fatal(err)
	}
	remasked := false
	for _, c := range stored {
		if c.Message == "first\n\n[REDACTED] line" {
			remasked = true
			if c.Files[0].Name != "logo.png" {
				t.Errorf("file name masked: %+v", c.Files)
			}
		}
	}
	if !remasked {
		t.Errorf("mask add did not reach stored commits: %+v", stored)
	}
}
