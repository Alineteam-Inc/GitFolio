package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
