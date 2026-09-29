package main

import "testing"

func TestMask(t *testing.T) {
	m := newMasker([]string{"acme", "고객사"})
	for in, want := range map[string]string{
		"see https://jira.acme.com/browse/PAY-12 for details":       "see [URL] for details",
		"contact jane.doe@acme.com":                                 "contact [EMAIL]",
		"Co-Authored-By: Claude <noreply@anthropic.com>":            "Co-Authored-By: Claude <[EMAIL]>",
		"fix PAY-123 and #45":                                       "fix [TICKET] and [TICKET]",
		"support UTF-8 and SHA-256":                                 "support UTF-8 and SHA-256",
		"server 10.0.0.12 down":                                     "server [IP] down",
		"token ghp_abcdefghijklmnopqrstuvwxyz0123":                  "token [SECRET]",
		"key AKIAIOSFODNN7EXAMPLE":                                  "key [SECRET]",
		"token aln_cli_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde": "token [SECRET]",
		"Acme 고객사 대응":                                               "[REDACTED] [REDACTED] 대응",
		"client_acme.go":                                            "client_[REDACTED].go",
		"internal-refactoring-of-the-module":                        "internal-refactoring-of-the-module",
		"plain message":                                             "plain message",
	} {
		got := m.apply(in)
		if got != want {
			t.Errorf("apply(%q) = %q, want %q", in, got, want)
		}
		if again := m.apply(got); again != got {
			t.Errorf("apply is not idempotent for %q: %q -> %q", in, got, again)
		}
	}
}
