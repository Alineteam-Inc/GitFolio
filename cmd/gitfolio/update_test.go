package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateCheck(t *testing.T) {
	for _, c := range []struct {
		tag, have string
		want      bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"v0.1.1", "0.1.0", true},
		{"v1.0.0", "0.9.9", true},
		{"v0.10.0", "0.9.0", true}, // numbers, not text
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.2.0", false},
		{"v0.2.0-rc.1", "0.1.0", false}, // pre-releases are not offered
		{"v0.2.0", "dev", false},
	} {
		if got := newer(c.tag, c.have); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.tag, c.have, got, c.want)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/Alineteam-Inc/GitFolio/releases/tag/v0.2.0", http.StatusFound)
	}))
	defer srv.Close()
	defer func(u string) { latestURL = u }(latestURL)
	latestURL = srv.URL
	if tag, err := latestRelease(); err != nil || tag != "v0.2.0" {
		t.Errorf("latestRelease = %q, %v", tag, err)
	}
	// Builds from source never check or ask.
	if checkUpdate(t.TempDir()) {
		t.Error("a dev build offered an update")
	}

	// gitfolio update: nothing to do from source or on the newest release, and today's offer is used up.
	dir := t.TempDir()
	if err := cmdUpdate(dir); err != nil {
		t.Errorf("update from a source build: %v", err)
	}
	defer func(v string) { version = v }(version)
	version = "0.2.0"
	if err := cmdUpdate(dir); err != nil {
		t.Errorf("update on the newest release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, updateFile)); err != nil {
		t.Error("update did not record the check")
	}
}

// The null device is a character device too, but nobody answers there.
func TestNullIsNotATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if terminal(f) {
		t.Error("the null device counts as a terminal")
	}
}

type ttyBuffer struct{ strings.Builder }

func (*ttyBuffer) Close() error { return nil }

// Someone who only pushes is told about a newer release in the terminal the push runs in, at most once
// a day, from what the push's background sync looked up: a push never waits on the network.
func TestUpdateNoticeOnPush(t *testing.T) {
	looks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		looks++
		http.Redirect(w, r, "/Alineteam-Inc/GitFolio/releases/tag/v0.5.0", http.StatusFound)
	}))
	defer srv.Close()
	defer func(u string) { latestURL = u }(latestURL)
	latestURL = srv.URL
	defer func(v string) { version = v }(version)
	version = "0.4.1"
	t.Setenv("GITFOLIO_LANG", "en")
	tty := &ttyBuffer{}
	defer func(f func() (io.WriteCloser, error)) { ttyOut = f }(ttyOut)
	ttyOut = func() (io.WriteCloser, error) { return tty, nil }
	dir := t.TempDir()

	noticeUpdate(dir) // nothing looked up yet
	if tty.Len() != 0 {
		t.Fatalf("told before any look-up: %q", tty.String())
	}
	if st := lookUpLatest(dir); st.Latest != "v0.5.0" || looks != 1 {
		t.Fatalf("look-up = %+v after %d requests", st, looks)
	}
	lookUpLatest(dir) // the same day: no second request
	if looks != 1 {
		t.Errorf("looked up %d times in a day", looks)
	}
	noticeUpdate(dir)
	if got := tty.String(); !strings.Contains(got, "0.5.0") || !strings.Contains(got, "0.4.1") || !strings.Contains(got, "gitfolio update") {
		t.Errorf("notice = %q", got)
	}
	told := tty.Len()
	noticeUpdate(dir) // once a day
	if tty.Len() != told {
		t.Errorf("told twice in a day: %q", tty.String())
	}
	// The notice does not use up the question an interactive command asks the same day.
	var st updateState
	if err := loadJSON(filepath.Join(dir, updateFile), &st); err != nil || !st.AskedAt.IsZero() {
		t.Errorf("state after the notice: %+v, %v", st, err)
	}

	// No terminal (a GUI git client): nothing is shown, and the next push tries again.
	dir = t.TempDir()
	lookUpLatest(dir)
	ttyOut = func() (io.WriteCloser, error) { return nil, os.ErrNotExist }
	noticeUpdate(dir)
	if err := loadJSON(filepath.Join(dir, updateFile), &st); err != nil || !st.NoticedAt.IsZero() {
		t.Errorf("without a terminal the notice counted as given: %+v", st)
	}
	// On the newest release, or built from source: nothing to tell.
	ttyOut = func() (io.WriteCloser, error) { return tty, nil }
	told = tty.Len()
	for _, v := range []string{"0.5.0", "dev"} {
		version = v
		noticeUpdate(t.TempDir())
		noticeUpdate(dir)
	}
	if tty.Len() != told {
		t.Errorf("told on the newest release or a source build: %q", tty.String()[told:])
	}
}
