package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestHooksEndToEnd installs the hooks with the real binary in a repository that already has a
// pre-push hook, then checks that an agent-made commit is collected and sent to aline.team (a fake
// server) after git push, that the old hook still runs, and that remove restores it.
func TestHooksEndToEnd(t *testing.T) {
	bin := t.TempDir()
	exe := "gitfolio"
	if runtime.GOOS == "windows" {
		exe += ".exe" // hooks run under Git for Windows' sh, which finds gitfolio.exe on PATH
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, exe), ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	home := t.TempDir()
	t.Setenv("HOME", home) // data dir comes from os.UserConfigDir
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for k := range agentEnv { // the test itself may run inside an AI agent
		t.Setenv(k, "")
	}
	t.Setenv("AI_AGENT", "")
	t.Setenv("AGENT", "")
	f := &fakeAline{token: testToken}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)

	remoteDir, repo := t.TempDir(), t.TempDir()
	run := func(env []string, dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir, cmd.Env = dir, append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run(nil, remoteDir, "git", "init", "-q", "--bare", "-b", "main")
	run(nil, repo, "git", "init", "-q", "-b", "main")
	run(nil, repo, "git", "config", "user.email", "me@example.com")
	run(nil, repo, "git", "config", "user.name", "me")
	// The fetch URL names a GitHub repository (its namespace is what aline.team gets); pushes go to remoteDir.
	run(nil, repo, "git", "remote", "add", "origin", "https://github.com/me/demo.git")
	run(nil, repo, "git", "remote", "set-url", "--push", "origin", remoteDir)
	hooks := filepath.Join(repo, ".git", "hooks")
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\ntouch \"$(dirname \"$0\")/orig-ran\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(nil, repo, "git", "add", ".")
	run(nil, repo, "git", "commit", "-q", "-m", "plain")
	run(nil, repo, "git", "push", "-q", "-u", "origin", "main")

	dir, err := dataDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveCredentials(dir, Credentials{Token: testToken}); err != nil { // nothing is collected before login
		t.Fatal(err)
	}
	run(nil, repo, "gitfolio", "add", ".")
	if b, _ := os.ReadFile(filepath.Join(hooks, "pre-push")); !strings.Contains(string(b), hookMarker) {
		t.Fatal("pre-push hook not installed")
	}
	os.Remove(filepath.Join(hooks, "orig-ran"))

	// A commit made from an AI agent's shell: no trailer, only the agent's environment variable.
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(nil, repo, "git", "add", ".")
	run([]string{"CLAUDECODE=1"}, repo, "git", "commit", "-q", "-m", "agent change")
	run(nil, repo, "git", "push", "-q", "origin", "main")
	if _, err := os.Stat(filepath.Join(hooks, "orig-ran")); err != nil {
		t.Error("the pre-existing pre-push hook did not run")
	}

	var got *Commit
	for deadline := time.Now().Add(15 * time.Second); got == nil && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		commits, _ := readCommits(dir)
		for i := range commits {
			if commits[i].Message == "agent change" {
				got = &commits[i]
			}
		}
	}
	if got == nil {
		t.Fatal("pushed commit was not collected by the pre-push hook")
	}
	if got.CreationType != "HUMAN_CO_AI" || strings.Join(got.AIAgents, ",") != "claude-code" {
		t.Errorf("agent change = %s %v, want HUMAN_CO_AI [claude-code]", got.CreationType, got.AIAgents)
	}
	sent := 0 // both commits: the one collected by add and the pushed one
	for deadline := time.Now().Add(15 * time.Second); sent < 2 && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		f.mu.Lock()
		sent = len(f.commits)
		_, pushed := f.commits["GITHUB/me/demo/"+got.Hash]
		f.mu.Unlock()
		if sent == 2 && !pushed {
			t.Fatal("the pushed commit is not among those sent")
		}
	}
	if sent != 2 {
		t.Fatalf("aline.team got %d commits after the push, want 2", sent)
	}

	run(nil, repo, "gitfolio", "remove", ".")
	b, _ := os.ReadFile(filepath.Join(hooks, "pre-push"))
	if strings.Contains(string(b), hookMarker) || !strings.Contains(string(b), "orig-ran") {
		t.Errorf("remove did not restore the original pre-push hook:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(hooks, "post-commit")); err == nil {
		t.Error("post-commit hook left behind after remove")
	}
}

// Before login nothing is collected: collecting commands fail with a hint and hooks do nothing,
// while settings and cleanup still work (DESIGN 6.1).
func TestLoginGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDECODE", "1") // post-commit would record this tag if it ran
	t.Chdir(t.TempDir())        // not a git repository: a hook that ran would fail
	if err := run([]string{"scan"}); err == nil || !strings.Contains(err.Error(), "gitfolio login") {
		t.Errorf("scan before login: %v, want a login hint", err)
	}
	if err := run([]string{"deps", "review"}); err == nil {
		t.Error("deps review ran before login")
	}
	for _, args := range [][]string{{"hook", "post-commit"}, {"hook", "push-wait"}, {"config", "api-url", "default"}, {"deps", "off"}} {
		if err := run(args); err != nil {
			t.Errorf("%v before login: %v", args, err)
		}
	}
}
