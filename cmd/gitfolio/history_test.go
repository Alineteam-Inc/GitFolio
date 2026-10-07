package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestGitHistory records the git commands of a run through GIT_TRACE, shows them per run, drops the
// oldest runs past the size limit, leaves a GIT_TRACE the user set alone, and deletes the record when
// turned off.
func TestGitHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_TRACE", "")
	dir, err := dataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gitTrace, historyOnce = "", sync.Once{} })

	startHistory(dir, []string{"scan", "--all"})
	if _, err := git(t.TempDir(), "version"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, historyFile)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	runs := parseHistory(b)
	if len(runs) != 1 || !strings.HasSuffix(runs[0].header, "  gitfolio scan --all") ||
		len(runs[0].git) != 1 || !strings.HasSuffix(runs[0].git[0], "  git version") {
		t.Fatalf("history = %+v\nfile:\n%s", runs, b)
	}

	// Past the limit the oldest runs go, and what is kept starts at a run's header.
	var old strings.Builder
	for i := range 400 {
		old.WriteString(historyHeader + "2026-01-01T00:00:00Z gitfolio sync\n")
		old.WriteString("10:00:00.000000 git.c:463 trace: built-in: git rev-parse HEAD " + strings.Repeat("x", i%7) + "\n")
	}
	if err := os.WriteFile(p, []byte(old.String()+string(b)), 0o600); err != nil {
		t.Fatal(err)
	}
	trimHistory(p, 16<<10)
	b, _ = os.ReadFile(p)
	if len(b) > 16<<10 || !strings.HasPrefix(string(b), historyHeader) || !strings.Contains(string(b), "gitfolio scan --all") {
		t.Errorf("after trimming: %d bytes, starts %q, newest run kept: %v", len(b), b[:min(len(b), 40)], strings.Contains(string(b), "scan --all"))
	}

	// A GIT_TRACE the user set wins: nothing is added here.
	t.Setenv("GIT_TRACE", filepath.Join(home, "mine.txt"))
	before := len(b)
	if _, err := git(t.TempDir(), "version"); err != nil {
		t.Fatal(err)
	}
	if b, _ = os.ReadFile(p); len(b) != before {
		t.Error("the history was written while the user's own GIT_TRACE was set")
	}
	if _, err := os.Stat(filepath.Join(home, "mine.txt")); err != nil {
		t.Error("the user's GIT_TRACE file was not written")
	}

	if err := run([]string{"config", "git-history", "off"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("turning the history off did not delete it")
	}
	if cfg, _ := loadConfig(dir); !cfg.GitHistoryOff {
		t.Error("git-history off was not saved")
	}
}
