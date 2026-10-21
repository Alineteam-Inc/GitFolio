package main

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"
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
		"portfolo":  {"portfolio"},
		"protfolio": {"portfolio"},
		"port":      {"portfolio", "export"},
	} {
		if got := similarCommands(in); !slices.Equal(got, want) {
			t.Errorf("similarCommands(%q) = %v, want %v", in, got, want)
		}
	}
	if err := run([]string{"stauts"}); err == nil {
		t.Error("a mistyped command ran")
	}
}

// A mistyped word or flag after a command names the ones meant too, and nothing runs.
func TestWrongWords(t *testing.T) {
	t.Setenv("GITFOLIO_LANG", "en")
	dir := t.TempDir()
	for _, c := range []struct {
		cmd  func(string, []string) error
		args []string
		want string
	}{
		{cmdConfig, []string{"autosyn", "off"}, "Did you mean `gitfolio config autosync`?"},
		{cmdConfig, []string{"autosync", "of"}, "`gitfolio config autosync off`"},
		{cmdConfig, []string{"mask", "ad", "acme"}, "`gitfolio config mask add`"},
		{cmdConfig, []string{"autosync"}, "Usage: gitfolio config"},
		{cmdConfig, []string{"xyz"}, "Usage: gitfolio config"},
		{cmdDeps, []string{"scna"}, "`gitfolio deps scan`"},
		{cmdDeps, []string{"auto", "of"}, "`gitfolio deps auto off`"},
		{cmdEmail, []string{"ad", "a@example.com"}, "`gitfolio email add`"},
		{cmdSchedule, []string{"of"}, "`gitfolio schedule off`"},
		{cmdSchedule, []string{"25:00"}, "Usage: gitfolio schedule"},
		{cmdStats, []string{"--refrsh"}, "`gitfolio stats --refresh`"},
		{cmdSync, []string{"--dryrun"}, "`gitfolio sync` takes no \"--dryrun\". Did you mean `gitfolio sync --dry-run`?"},
		{cmdSync, []string{"-h"}, "Usage: gitfolio sync [--dry-run]"},
		{cmdRemove, []string{"--prge"}, "`gitfolio remove --purge`"},
		{cmdScan, []string{"--al"}, "`gitfolio scan --all`"},
		{cmdHistory, []string{"--al"}, "`gitfolio history --all`"},
		{cmdPortfolio, []string{"--form", "cv.md"}, "`gitfolio portfolio --from`"},
		{cmdPortfolio, []string{"--from"}, "Usage: gitfolio portfolio"},
	} {
		if err := c.cmd(dir, c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.args, err, c.want)
		}
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
