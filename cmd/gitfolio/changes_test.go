package main

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSystemCommit(t *testing.T) {
	for subject, want := range map[string]bool{
		"merge: main into feature":     true,
		"Merge branch 'main'":          true,
		"Release: v1.2":                true,
		"release v1.2.0":               true,
		"Cherry-pick fix from main":    true,
		"rebase onto main":             true,
		"Initial commit":               true,
		"auto":                         true,
		"chore(release): v1.2":         false, // CHORE on the server
		"revert: bad change":           false, // REVERT counts as a change
		"feat!: new api":               false,
		"fix the release script":       false,
		"initial support for windows":  false, // longer than a short system subject
		"Add bot detection for agents": false,
		"":                             false,
	} {
		if got := systemCommit(subject + "\n\nbody"); got != want {
			t.Errorf("systemCommit(%q) = %v, want %v", subject, got, want)
		}
	}
}

// TestFirstChanges builds a history where others change, rename and "release" files the user created.
func TestFirstChanges(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, data := t.TempDir(), t.TempDir()
	commit := func(email, date, msg string, files map[string]string, mv ...string) {
		t.Helper()
		for name, body := range files {
			p := filepath.Join(repo, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		env := append(os.Environ(), "GIT_AUTHOR_NAME=x", "GIT_AUTHOR_EMAIL="+email, "GIT_AUTHOR_DATE="+date,
			"GIT_COMMITTER_NAME=x", "GIT_COMMITTER_EMAIL="+email, "GIT_COMMITTER_DATE="+date)
		for _, args := range [][]string{append([]string{"mv"}, mv...), {"add", "-A"}, {"commit", "-q", "-m", msg}} {
			if args[0] == "mv" && len(mv) == 0 {
				continue
			}
			cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "user.email", "me@example.com").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	me, other := "me@example.com", "other@example.com"
	commit(me, "2026-09-01T09:00:00+09:00", "feat: start", map[string]string{"src/a.go": "a\n", "src/b.go": "b\n", "c.go": "c\n"})
	commit(other, "2026-09-02T10:00:00+09:00", "fix: a", map[string]string{"src/a.go": "a2\n"})
	commit(other, "2026-09-03T10:00:00+09:00", "Release: v1.0", map[string]string{"src/b.go": "b2\n"}) // system: not a change
	commit(other, "2026-09-04T10:00:00+09:00", "refactor: move c", nil, "c.go", "d.go")                // a rename: not followed
	commit(other, "2026-09-05T10:00:00+09:00", "tweak b", map[string]string{"src/b.go": "b3\n"})
	commit(other, "2026-09-06T10:00:00+09:00", "fix: a again", map[string]string{"src/a.go": "a3\n"}) // not the first change

	r := newRepo(repo)
	if _, err := scanRepo(data, &r, false); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"src/a.go": "2026-09-02T10:00:00+09:00", "src/b.go": "2026-09-05T10:00:00+09:00"}
	if !maps.Equal(r.Changed, want) {
		t.Errorf("first changes = %v, want %v", r.Changed, want)
	}
}
