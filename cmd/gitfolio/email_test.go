package main

import (
	"os"
	"os/exec"
	"slices"
	"testing"
)

func TestEmailCommands(t *testing.T) {
	dir := t.TempDir()
	emails := func() []string {
		t.Helper()
		cfg, err := loadConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Emails
	}
	for _, args := range [][]string{{"add", "Me@Home.com", "me@work.com"}, {"add", "me@work.com"}, {"primary", "me@work.com"}, {"rm", "me@home.com"}} {
		if err := cmdEmail(dir, args); err != nil {
			t.Fatalf("email %v: %v", args, err)
		}
	}
	if got := emails(); !slices.Equal(got, []string{"me@work.com"}) {
		t.Errorf("emails = %v, want [me@work.com] (lowercased, no duplicates, primary first)", got)
	}
	for _, bad := range [][]string{{"rm", "me@work.com"}, {"rm", "nobody@x.com"}, {"add", "not-an-email"}, {"primary", "a b@x.com"}, {"frob", "x@y.z"}} {
		if cmdEmail(dir, bad) == nil {
			t.Errorf("email %v succeeded", bad)
		}
	}
	if got := emails(); !slices.Equal(got, []string{"me@work.com"}) {
		t.Errorf("a refused command changed the emails: %v", got)
	}
}

// Commits by any work email or by the repository's git email are the user's; the first repository
// registered brings its git email in as the primary one.
func TestWorkEmails(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, dir := t.TempDir(), t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "Repo@Example.com"}, {"config", "user.name", "me"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mine, err := myEmails(repo, []string{"Work@Example.com"})
	if err != nil || !mine["work@example.com"] || !mine["repo@example.com"] || len(mine) != 2 {
		t.Errorf("myEmails = %v, %v", mine, err)
	}
	if err := registerRepo(dir, repo); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := loadConfig(dir); !slices.Equal(cfg.Emails, []string{"repo@example.com"}) || primaryEmail(cfg) != "repo@example.com" {
		t.Errorf("after the first add: emails %v, primary %q", cfg.Emails, primaryEmail(cfg))
	}
}
