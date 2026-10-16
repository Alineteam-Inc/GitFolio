package main

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCommitType holds examples of aline.team's commit types; the same ones are to give the same type there.
func TestCommitType(t *testing.T) {
	for msg, want := range map[string]string{
		"merge: main into feature":     "SYSTEM",
		"Merge branch 'main'":          "SYSTEM",
		"Release: v1.2":                "SYSTEM", // no Conventional Commits type, then a first word
		"release v1.2.0":               "SYSTEM",
		"rebase onto main":             "SYSTEM",
		"Initial commit":               "SYSTEM",
		"initial support for windows":  "SYSTEM", // no keyword ranks above initial
		"auto":                         "SYSTEM",
		"Cherry-pick fix from main":    "FIX", // the hyphen splits the first word
		"chore(release): v1.2":         "CHORE",
		"revert: bad change":           "REVERT",
		"feat!: new api":               "ADD",
		"feat(): empty scope":          "ADD", // no prefix match; feat is still a keyword
		"fix the release script":       "FIX",
		"Add bot detection for agents": "ADD",
		"Fix security issue in login":  "HOTFIX",        // the highest ranked keyword
		"update README":                "DOCUMENTATION", // the higher of update and readme
		"버그 수정":                        "FIX",
		"로그인 기능 추가":                    "ADD",
		"Title: A+B, Time: 56 ms, Memory: 31120 KB -BaekjoonHub": "ADD",
		"tweak b": "UNCATEGORIZED",
		"":        "UNCATEGORIZED",
		"!!!":     "UNCATEGORIZED",
		"docs: guide\n\nfix: body is not the subject": "DOCUMENTATION",
		"tidy\r\n\r\nfix later":                       "CHORE",
		"use index\nfix":                              "FIX", // a single line break keeps one subject
	} {
		if got := commitType(msg); got != want {
			t.Errorf("commitType(%q) = %s, want %s", msg, got, want)
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

	if out, err := exec.Command("git", "-C", repo, "update-ref", "refs/remotes/origin/main", "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out) // pushed
	}
	r := newRepo(repo)
	if _, err := scanRepo(data, &r, false); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"src/a.go": "2026-09-02T10:00:00+09:00", "src/b.go": "2026-09-05T10:00:00+09:00"}
	if !maps.Equal(r.Changed, want) {
		t.Errorf("first changes = %v, want %v", r.Changed, want)
	}
	// The same history as later changes to the user's files: the release does not count, the rename is
	// not followed, and a file changed again within 14 days counts once, on the first such day.
	wantChurn := map[string]churn{
		"2026-09-02": {Touches: pair{1}, Corrective: pair{1}, Early: pair{1}},
		"2026-09-05": {Touches: pair{1}, Early: pair{1}},
		"2026-09-06": {Touches: pair{1}, Corrective: pair{1}},
	}
	if !maps.Equal(r.Churn, wantChurn) || r.AsOf != "2026-09-06T01:00:00Z" {
		t.Errorf("churn = %v as of %s, want %v as of 2026-09-06T01:00:00Z", r.Churn, r.AsOf, wantChurn)
	}
}
