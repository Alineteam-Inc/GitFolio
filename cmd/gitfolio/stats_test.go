package main

import (
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitfolio stats gets the developer type from aline.team the first time, keeps it, and shows the kept
// one after that: again only with --refresh, for another account, or when nothing was there to keep.
func TestStats(t *testing.T) {
	f := &fakeAline{token: testToken, devType: map[string]any{
		"devTypeTitle": "ARCHITECT", "devTypeDescription": "Designs systems", "devTypeComputedAt": "2026-10-05T09:00:00",
		"agilityStat": 72, "agilityStatAi": 65, "stabilityStat": 80, "devTypeTitleAi": "PIONEER", "dominantPosition": "BACKEND",
		"userEmail": "dev@example.com", "sharedCode": "shareXYZ", "devTypeReason": "because",
	}}
	ts := httptest.NewServer(f.handler())
	defer ts.Close()
	srv := ts.URL
	t.Setenv("GITFOLIO_API_URL", srv)
	t.Setenv("GITFOLIO_LANG", "en")
	var buf bytes.Buffer
	defer func(o, e io.Writer) { out, errOut = o, e }(out, errOut)
	out, errOut = &buf, &buf
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: srv}))

	must(cmdStats(dir, nil))
	if f.devTypeCalls != 1 || !strings.Contains(buf.String(), "ARCHITECT") || !strings.Contains(buf.String(), "72") ||
		!strings.Contains(buf.String(), "AI 65") || !strings.Contains(buf.String(), "PIONEER") {
		t.Fatalf("first run: %d calls, shown:\n%s", f.devTypeCalls, buf.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, devTypeFile)); !strings.Contains(string(b), "ARCHITECT") ||
		strings.Contains(string(b), "shareXYZ") || strings.Contains(string(b), "userEmail") || strings.Contains(string(b), "because") {
		t.Errorf("kept more than is shown:\n%s", b)
	}
	buf.Reset()
	must(cmdStats(dir, nil)) // kept: no request
	if f.devTypeCalls != 1 || !strings.Contains(buf.String(), "ARCHITECT") || !strings.Contains(buf.String(), "--refresh") {
		t.Errorf("second run: %d calls, shown:\n%s", f.devTypeCalls, buf.String())
	}
	must(cmdStats(dir, []string{"--refresh"}))
	if f.devTypeCalls != 2 {
		t.Errorf("--refresh made %d calls in all, want 2", f.devTypeCalls)
	}
	if cmdStats(dir, []string{"--all"}) == nil {
		t.Error("an unknown flag was taken")
	}

	// aline.team is down: the kept one is shown, with a warning.
	f.down = true
	buf.Reset()
	must(cmdStats(dir, []string{"--refresh"}))
	if !strings.Contains(buf.String(), "ARCHITECT") || !strings.Contains(buf.String(), "Could not get it") {
		t.Errorf("offline refresh shows:\n%s", buf.String())
	}
	f.down = false

	// Another account on this computer: its own type is fetched.
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "other@example.com", Server: srv}))
	must(cmdStats(dir, nil))
	if f.devTypeCalls != 3 { // the refused refresh never reached it
		t.Errorf("another account made %d calls in all, want 3", f.devTypeCalls)
	}

	// Nothing to analyse yet: said so, and nothing is kept, so the next run asks again.
	dir = t.TempDir()
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: srv}))
	f.devType = nil
	buf.Reset()
	must(cmdStats(dir, nil))
	must(cmdStats(dir, nil))
	if f.devTypeCalls != 5 || !strings.Contains(buf.String(), "nothing to make a developer type from") {
		t.Errorf("no data: %d calls in all, shown:\n%s", f.devTypeCalls, buf.String())
	}
	// Made too slowly, nothing kept: aline.team finishes it, so try again shortly.
	f.devTypeWait = 200 * time.Millisecond
	timeout := apiTimeout
	defer func() { apiTimeout = timeout }()
	apiTimeout = 50 * time.Millisecond
	if err := cmdStats(dir, nil); err == nil || !strings.Contains(err.Error(), "still making") {
		t.Errorf("timeout: %v", err)
	}

	// Not logged in, nothing kept: log in first.
	if err := cmdStats(t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "login") {
		t.Errorf("logged out: %v", err)
	}

	// Local mode: the statistics go first, without commit records, and the type is asked for every time.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f.devTypeWait, f.devType, apiTimeout = 0, map[string]any{"devTypeTitle": "BUILDER"}, timeout
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"remote", "add", "origin", "https://github.com/me/app.git"},
		{"-c", "user.email=dev@example.com", "-c", "user.name=x", "commit", "-q", "--allow-empty", "-m", "feat: start"},
		{"update-ref", "refs/remotes/origin/main", "HEAD"}} {
		if b, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	dir = t.TempDir()
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: srv}))
	must(saveConfig(dir, Config{Emails: []string{"dev@example.com"}, AutoSyncOff: true}))
	must(saveRepos(dir, []Repo{newRepo(repo)}))
	calls := f.devTypeCalls
	must(cmdStats(dir, nil))
	must(cmdStats(dir, nil))
	if _, ok := f.stats["GITHUB/me/app"]; !ok || len(f.commits) != 0 || f.devTypeCalls != calls+2 {
		t.Errorf("local mode: snapshot sent %v, %d commits sent, %d type requests; want the snapshot only and 2 requests",
			ok, len(f.commits), f.devTypeCalls-calls)
	}
}

// Each developer type has its picture, 11 squares wide in known colors, drawn only in color.
func TestDevTypeArt(t *testing.T) {
	for _, title := range []string{"BUILDER", "EXPLORER", "FIXER", "KEEPER", "LEADER", "SPRINTER"} {
		art := devTypeArt[title]
		if len(art) == 0 {
			t.Errorf("%s has no picture", title)
		}
		for _, row := range art {
			bad := strings.IndexFunc(row, func(c rune) bool { _, ok := devTypeColors[c]; return !ok && c != '.' })
			if len(row) != 11 || bad >= 0 {
				t.Errorf("%s row %q", title, row)
			}
		}
	}
	var buf bytes.Buffer
	defer func(o io.Writer, c bool) { out, useColor = o, c }(out, useColor)
	out = &buf
	useColor = false
	drawDevType("EXPLORER")
	if buf.Len() != 0 {
		t.Errorf("drawn without color:\n%s", buf.String())
	}
	useColor = true
	drawDevType("EXPLORER")
	// two squares a line: blue over yellow where the window starts
	if n := strings.Count(buf.String(), "\n"); n != (len(devTypeArt["EXPLORER"])+1)/2 || !strings.Contains(buf.String(), "\x1b[38;5;68;48;5;187m▀") {
		t.Errorf("EXPLORER drawn in %d lines:\n%s", n, buf.String())
	}
	n := buf.Len()
	if drawDevType("UNKNOWN"); buf.Len() != n { // a type this version does not know
		t.Error("an unknown type was drawn")
	}
}

// stack shows aline.team's tech stack periods in one table, asked for each time.
func TestStack(t *testing.T) {
	f := &fakeAline{token: testToken, techUsages: map[string]any{
		"languages":  []map[string]any{{"name": "Go", "startDate": "2025-03-02T10:00:00", "endDate": "2026-10-05T09:00:00", "careerMonths": 20, "projectCount": 3}},
		"frameworks": []map[string]any{{"name": "Spring Boot", "framework": "SPRING_BOOT", "startDate": "2024-01-10T10:00:00", "endDate": "2025-12-01T10:00:00", "careerMonths": 23, "projectCount": 1, "isHidden": true}},
		"libraries":  []map[string]any{},
	}}
	ts := httptest.NewServer(f.handler())
	defer ts.Close()
	t.Setenv("GITFOLIO_API_URL", ts.URL)
	t.Setenv("GITFOLIO_LANG", "en")
	var buf bytes.Buffer
	defer func(o, e io.Writer) { out, errOut = o, e }(out, errOut)
	out, errOut = &buf, &buf
	dir := t.TempDir()
	if err := saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: ts.URL}); err != nil {
		t.Fatal(err)
	}
	if err := cmdStack(dir, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"KIND", "language", "Go", "2025-03", "2026-10", "framework", "Spring Boot *", "2024-01", "* hidden on your aline.team profile"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("stack shows no %q:\n%s", want, buf.String())
		}
	}
	f.techUsages = map[string]any{"languages": []any{}, "frameworks": []any{}, "libraries": []any{}}
	buf.Reset()
	if err := cmdStack(dir, nil); err != nil || !strings.Contains(buf.String(), "no tech stack") {
		t.Errorf("empty stack: %v\n%s", err, buf.String())
	}
	if cmdStack(dir, []string{"--all"}) == nil {
		t.Error("an argument was taken")
	}
}
