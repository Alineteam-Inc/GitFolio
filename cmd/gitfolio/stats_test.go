package main

import (
	"bytes"
	"io"
	"net/http/httptest"
	"os"
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
	defer func(d time.Duration) { apiTimeout = d }(apiTimeout)
	apiTimeout = 50 * time.Millisecond
	if err := cmdStats(dir, nil); err == nil || !strings.Contains(err.Error(), "still making") {
		t.Errorf("timeout: %v", err)
	}

	// Not logged in, nothing kept: log in first.
	if err := cmdStats(t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "login") {
		t.Errorf("logged out: %v", err)
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
	if n := strings.Count(buf.String(), "\n"); n != len(devTypeArt["EXPLORER"]) || !strings.Contains(buf.String(), "\x1b[38;5;68m██") {
		t.Errorf("EXPLORER drawn in %d lines:\n%s", n, buf.String())
	}
	n := buf.Len()
	if drawDevType("UNKNOWN"); buf.Len() != n { // a type this version does not know
		t.Error("an unknown type was drawn")
	}
}
