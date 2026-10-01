package main

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Files are sent as git diff and log show them: the path in the repository, the new side of a rename.
func TestNewPath(t *testing.T) {
	for in, want := range map[string]string{
		"src/a.go":           "src/a.go",
		"src/{a.go => b.go}": "src/b.go",
		"{src => lib}/x.go":  "lib/x.go",
		"{ => sub}/x.go":     "sub/x.go",
		"{src => }/x.go":     "x.go",
		"a/{b => }/c/x.go":   "a/c/x.go",
		"old.go => new.go":   "new.go",
		`"dir/tab\there.go"`: "dir/tab\there.go",
		"docs/한글.md":         "docs/한글.md",
	} {
		if got := newPath(in); got != want {
			t.Errorf("newPath(%q) = %q, want %q", in, got, want)
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
		// Azure DevOps: always organization/project/repository.
		"https://dev.azure.com/org/Proj/_git/repo":                          "DEVOPS org/Proj/repo",
		"https://org@dev.azure.com/org/My%20Project/_git/repo":              "DEVOPS org/My Project/repo",
		"git@ssh.dev.azure.com:v3/org/My%20Project/repo":                    "DEVOPS org/My Project/repo",
		"https://org.visualstudio.com/Proj/_git/repo":                       "DEVOPS org/Proj/repo",
		"https://org.visualstudio.com/DefaultCollection/Proj/_git/repo.git": "DEVOPS org/Proj/repo",
		"org@vs-ssh.visualstudio.com:v3/org/Proj/repo":                      "DEVOPS org/Proj/repo",
		"https://dev.azure.com/org/_git/repo":                               "DEVOPS org/repo/repo",
		"/srv/git/repo.git":                                                 " ",
		"file:///srv/git/repo.git":                                          " ",
		`C:\work\repo`:                                                      " ",
	} {
		provider, ns := parseRemote(in)
		if got := provider + " " + ns; got != want {
			t.Errorf("parseRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

// webURL builds a repository address from provider and namespace the way aline.team does (confirmed by
// the server 2026-09-29): each path segment is percent-encoded, "/" stays a separator.
func webURL(provider, namespace string) string {
	seg := strings.Split(namespace, "/")
	for i := range seg {
		seg[i] = url.PathEscape(seg[i])
	}
	switch provider {
	case "GITHUB":
		return "https://github.com/" + strings.Join(seg, "/")
	case "GITLAB":
		return "https://gitlab.com/" + strings.Join(seg, "/")
	case "BITBUCKET":
		return "https://bitbucket.org/" + strings.Join(seg, "/")
	case "DEVOPS":
		if len(seg) == 3 {
			return "https://dev.azure.com/" + seg[0] + "/" + seg[1] + "/_git/" + seg[2]
		}
		return "https://dev.azure.com/" + strings.Join(seg, "/")
	}
	return "" // OTHER has no address
}

// Whatever form a clone URL takes, the provider and namespace sent must give the address the git
// service's web UI shows when sharing the repository.
func TestRemoteGivesWebURL(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:Alineteam-Inc/GitFolio.git":                     "https://github.com/Alineteam-Inc/GitFolio",
		"https://x-access-token:ghp_secret@github.com/o/r.git":          "https://github.com/o/r",
		"git@github.com:me/me.github.io.git":                            "https://github.com/me/me.github.io",
		"https://github.com/me/me.github.io":                            "https://github.com/me/me.github.io",
		"ssh://git@ssh.github.com:443/o/r.git":                          "https://github.com/o/r",
		"git@gitlab.com:group/sub/project.git":                          "https://gitlab.com/group/sub/project",
		"https://oauth2:glpat-x@gitlab.com/group/project.git/":          "https://gitlab.com/group/project",
		"git@bitbucket.org:workspace/repo.git":                          "https://bitbucket.org/workspace/repo",
		"https://user@bitbucket.org/workspace/repo.git":                 "https://bitbucket.org/workspace/repo",
		"https://org@dev.azure.com/org/My%20Project/_git/repo":          "https://dev.azure.com/org/My%20Project/_git/repo",
		"https://dev.azure.com/org/%ED%95%9C%EA%B8%80/_git/repo":        "https://dev.azure.com/org/%ED%95%9C%EA%B8%80/_git/repo",
		"git@ssh.dev.azure.com:v3/org/My%20Project/repo":                "https://dev.azure.com/org/My%20Project/_git/repo",
		"https://org.visualstudio.com/DefaultCollection/Proj/_git/repo": "https://dev.azure.com/org/Proj/_git/repo",
		"org@vs-ssh.visualstudio.com:v3/org/Proj/repo":                  "https://dev.azure.com/org/Proj/_git/repo",
		"https://dev.azure.com/org/_git/repo":                           "https://dev.azure.com/org/repo/_git/repo",
		"https://gitlab.company.internal/group/project.git":             "",
	} {
		if got := webURL(parseRemote(remote)); got != want {
			t.Errorf("%s → %q, want %q", remote, got, want)
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
	created := func(f FileStat) FileStat { f.Created = true; return f }
	want := map[string][]FileStat{
		"first\n\nbody line": {created(fs("logo.png", 0, 0)), created(fs("src/a.go", 3, 0))}, // numstat lists paths sorted
		"rename":             {fs("src/b.go", 0, 0)},                                         // a rename is not a created file
		side:                 {created(fs("c.txt", 1, 0))},
		botWork:              {created(fs("d.txt", 1, 0))},
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

// Commits stored by an older version, with file names only, are collected again once with their paths.
func TestOldFormatRescanned(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, data := t.TempDir(), t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "me@example.com")
	git("config", "user.name", "me")
	if err := os.MkdirAll(filepath.Join(repo, "cmd", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "cmd", "app", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "start")
	r := newRepo(repo)
	if _, err := scanRepo(data, &r, false); err != nil {
		t.Fatal(err)
	}
	commits, _ := readCommits(data)
	commits[0].Files[0].Name = "main.go" // as stored before paths were recorded
	if err := writeCommits(data, commits); err != nil {
		t.Fatal(err)
	}
	r.Format = 0
	if _, err := scanRepo(data, &r, false); err != nil {
		t.Fatal(err)
	}
	if commits, _ = readCommits(data); len(commits) != 1 || commits[0].Files[0].Name != "cmd/app/main.go" || r.Format != repoFormat {
		t.Errorf("after rescan: %+v, format %d", commits, r.Format)
	}
}
