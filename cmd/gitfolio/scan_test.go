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

	r := newRepo(repo)
	n, err := scanRepo(data, &r, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("first scan added %d commits, want 3 (first, rename, side)", n)
	}
	if n, err := scanRepo(data, &r, false); err != nil || n != 0 {
		t.Fatalf("rescan added %d commits (err %v), want 0", n, err)
	}
	if n, err := scanRepo(data, &r, true); err != nil || n != 3 {
		t.Fatalf("rebuild added %d commits (err %v), want 3", n, err)
	}

	stored, err := readCommits(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored %d commits after rebuild, want 3", len(stored))
	}
	byMsg := map[string]Commit{}
	for _, c := range stored {
		byMsg[c.Message] = c
		if c.AuthorEmail != "Me@Example.com" || len(c.Hash) != 40 {
			t.Errorf("%q: authorEmail %q, hash %q", c.Message, c.AuthorEmail, c.Hash)
		}
	}
	side := "side\n\nCo-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
	if c := byMsg[side]; !c.AIContributed || len(c.AIAgents) != 1 || c.AIAgents[0] != "claude-code" {
		t.Errorf("side commit ai = %v %v, want true [claude-code]", c.AIContributed, c.AIAgents)
	}
	if byMsg["rename"].AIContributed {
		t.Error("rename commit marked as AI-contributed")
	}
	want := map[string][]FileStat{
		"first\n\nbody line": {{"logo.png", 0, 0}, {"a.go", 3, 0}}, // numstat lists paths sorted: logo.png < src/a.go
		"rename":             {{"b.go", 0, 0}},
		side:                 {{"c.txt", 1, 0}},
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
}
