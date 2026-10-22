package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A mistyped command names the ones the user may have meant.
func TestSimilarCommands(t *testing.T) {
	for in, want := range map[string][]string{
		"-v":        {"version"},
		"--version": {"version"},
		"sta":       {"status", "stats", "stack"},
		"st":        {"status", "stats", "stack"},
		"stat":      {"status", "stats", "stack"}, // stack: one letter off
		"stauts":    {"status", "stats"},          // one swap from status, one letter from stats
		"lsit":      {"list"},
		"lgoin":     {"login"},
		"verison":   {"version"},
		"sinc":      {"sync"},
		"logot":     {"logout", "login"},
		"hook":      nil, // for git hooks only, never offered
		"xyz":       nil,
	} {
		if got := similarCommands(in); !slices.Equal(got, want) {
			t.Errorf("similarCommands(%q) = %v, want %v", in, got, want)
		}
	}
	if err := run([]string{"stauts"}); err == nil {
		t.Error("a mistyped command ran")
	}
}

// export, config and sync --dry-run print JSON two-space indented, with "<a@b.com>" kept readable.
func TestPrintJSON(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func(o *os.File) { os.Stdout = o }(os.Stdout)
	os.Stdout = w
	err = printJSON(map[string]any{"author": "Dev <dev@example.com>"})
	w.Close()
	got, _ := io.ReadAll(r)
	if err != nil || string(got) != "{\n  \"author\": \"Dev <dev@example.com>\"\n}\n" {
		t.Errorf("printJSON wrote %q (%v)", got, err)
	}
}

// jsonStdout runs fn and returns the one JSON document it printed on stdout (nil for none).
func jsonStdout(t *testing.T, fn func() error) (map[string]any, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func(o *os.File) { os.Stdout = o }(os.Stdout)
	os.Stdout = w
	err = fn()
	w.Close()
	b, _ := io.ReadAll(r)
	var v map[string]any
	if len(b) > 0 {
		if jerr := json.Unmarshal(b, &v); jerr != nil {
			t.Fatalf("not one JSON document: %v\n%s", jerr, b)
		}
	}
	return v, err
}

// uninstall unregisters every repository, puts back the hooks that were there before, revokes the
// login when aline.team answers and deletes it either way, keeps the data unless --purge-local, asks
// nothing, and can run again.
func TestUninstall(t *testing.T) {
	t.Setenv("GITFOLIO_LANG", "en")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := &fakeAline{token: testToken}
	ts := httptest.NewServer(f.handler())
	defer ts.Close()
	t.Setenv("GITFOLIO_API_URL", ts.URL)
	var buf bytes.Buffer
	defer func(o, e io.Writer, d time.Duration) { out, errOut, jsonOut, apiTimeout = o, e, false, d }(out, errOut, apiTimeout)
	out, errOut = &buf, &buf

	repo := t.TempDir()
	if b, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, b)
	}
	hooks := filepath.Join(repo, ".git", "hooks")
	before := "#!/bin/sh\necho before\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte(before), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installHooks(repo); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gone := filepath.Join(t.TempDir(), "deleted") // a registered folder deleted since
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(saveRepos(dir, []Repo{newRepo(repo), newRepo(gone)}))
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: ts.URL}))

	v, err := jsonStdout(t, func() error { return cmdUninstall(dir, []string{"--json"}) })
	list, _ := v["repositories"].([]any)
	if err != nil || len(list) != 2 || v["revoked"] != true || v["dataDeleted"] != false || f.token != "" {
		t.Fatalf("uninstall: %v %v (server token %q)", v, err, f.token)
	}
	if b, _ := os.ReadFile(filepath.Join(hooks, "pre-push")); string(b) != before {
		t.Errorf("pre-push not put back:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(hooks, "post-commit")); err == nil {
		t.Error("gitfolio's post-commit hook is still there")
	}
	repos, _ := loadRepos(dir)
	creds, _ := loadCredentials(dir)
	if len(repos) != 0 || creds.Token != "" {
		t.Errorf("after uninstall: %d repositories, token kept %v", len(repos), creds.Token != "")
	}

	// Again, offline and logged in once more: nothing to undo, the login goes all the same.
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: ts.URL}))
	ts.Close()
	v, err = jsonStdout(t, func() error { return cmdUninstall(dir, []string{"--json"}) })
	if list, _ := v["repositories"].([]any); err != nil || len(list) != 0 || v["revoked"] != false {
		t.Errorf("second uninstall: %v %v", v, err)
	}
	if creds, _ := loadCredentials(dir); creds.Token != "" {
		t.Error("an unrevoked login was kept")
	}

	jsonOut = false // one command per process otherwise
	must(cmdUninstall(dir, []string{"--purge-local"}))
	if _, err := os.Stat(dir); !os.IsNotExist(err) || !strings.Contains(buf.String(), "deleted too") {
		t.Errorf("--purge-local left %s (%v):\n%s", dir, err, buf.String())
	}
	if cmdUninstall(t.TempDir(), []string{"--purge"}) == nil {
		t.Error("an unknown flag was taken")
	}
}

// A failure before the command itself runs, such as the login a sync needs, comes as JSON too.
func TestJSONBeforeCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	defer func(o io.Writer, i func() bool) { out, jsonOut, interactive = o, false, i }(out, interactive)
	interactive = func() bool { return false }
	if err := run([]string{"sync", "--json"}); errorCode(err) != "loginFirst" || !jsonOut {
		t.Errorf("sync --json logged out: %v, JSON %v", err, jsonOut)
	}
}
