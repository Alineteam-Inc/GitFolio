package main

import (
	"slices"
	"testing"
)

// A mistyped command names the ones the user may have meant.
func TestSimilarCommands(t *testing.T) {
	for in, want := range map[string][]string{
		"-v":        {"version"},
		"--version": {"version"},
		"sta":       {"status"},
		"st":        {"status"},
		"stauts":    {"status"},
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
