package main

import (
	"io"
	"os"
	"slices"
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
