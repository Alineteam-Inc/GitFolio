package main

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestParseHHMM(t *testing.T) {
	for in, ok := range map[string]bool{"09:00": true, "0:05": true, "23:59": true, "24:00": false, "9:5": false, "09:60": false, "0900": false, "": false, "ab:cd": false} {
		if _, _, got := parseHHMM(in); got != ok {
			t.Errorf("parseHHMM(%q) ok = %v, want %v", in, got, ok)
		}
	}
}

func TestLaunchdPlist(t *testing.T) {
	p := launchdPlist("/Users/a&b/bin/gitfolio", "/tmp/log <1>", [][2]string{{"PATH", "/usr/bin:/bin"}, {"GITFOLIO_LANG", "ko"}}, 9, 5)
	dec := xml.NewDecoder(strings.NewReader(p))
	for {
		if _, err := dec.Token(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("plist is not well-formed XML: %v\n%s", err, p)
		}
	}
	for _, want := range []string{"<string>/Users/a&amp;b/bin/gitfolio</string>", "<string>sync</string>", "<key>Hour</key>\n\t\t<integer>9</integer>", "<key>Minute</key>\n\t\t<integer>5</integer>", "/tmp/log &lt;1&gt;", "<key>GITFOLIO_LANG</key>\n\t\t<string>ko</string>"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q:\n%s", want, p)
		}
	}
}

func TestSystemdUnits(t *testing.T) {
	service, timer := systemdUnits("/opt/my apps/gitfolio", [][2]string{{"PATH", "/usr/bin:/100%"}, {"GITFOLIO_LANG", "ko"}}, 9, 5)
	for _, want := range []string{`ExecStart="/opt/my apps/gitfolio" sync`, `Environment="PATH=/usr/bin:/100%%"`, `Environment="GITFOLIO_LANG=ko"`, "Type=oneshot"} {
		if !strings.Contains(service, want) {
			t.Errorf("service lacks %q:\n%s", want, service)
		}
	}
	for _, want := range []string{"OnCalendar=*-*-* 09:05:00", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer lacks %q:\n%s", want, timer)
		}
	}
}

// The user's own crontab lines, blank lines included, are kept; only GitFolio's line changes.
func TestCronWith(t *testing.T) {
	mine := "# my jobs\n0 1 * * * backup\n\n5 9 * * * 'old' sync " + cronMarker + "\n"
	line := "30 8 * * * 'new' sync " + cronMarker
	if got, want := cronWith(mine, line), "# my jobs\n0 1 * * * backup\n\n"+line+"\n"; got != want {
		t.Errorf("replace:\n%q\nwant\n%q", got, want)
	}
	if got, want := cronWith(mine, ""), "# my jobs\n0 1 * * * backup\n\n"; got != want {
		t.Errorf("remove:\n%q\nwant\n%q", got, want)
	}
	if got := cronWith("", line); got != line+"\n" {
		t.Errorf("empty crontab: %q", got)
	}
	if got := cronWith("5 9 * * * 'old' sync "+cronMarker+"\n", ""); got != "" {
		t.Errorf("only ours removed: %q", got)
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("shellQuote = %s", got)
	}
}
