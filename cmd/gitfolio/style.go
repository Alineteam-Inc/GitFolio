package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// What people read is set apart by kind (DESIGN 7.1): results start with a green "GitFolio >>",
// problems with a yellow one, questions with a cyan "?" and bold text and a dim input hint, settings
// are aligned "label  value" lines, and explanations are plain text. Paragraphs are separated by
// exactly one empty line, wherever they are printed from. Color is used only in a terminal, and
// never with NO_COLOR set (https://no-color.org).

// screen is stdout or stderr, remembering how the last output ended so gap knows whether an empty
// line is already there. Both share the count: they end up on the same terminal.
type screen struct{ w io.Writer }

// newlines counts the newlines that ended the last output: 0 = mid-line, 1 = at the start of a line,
// 2 or more = after an empty line (or before anything was printed).
var newlines = 2

func (s screen) Write(p []byte) (int, error) {
	if len(p) > 0 {
		k := len(p) - len(strings.TrimRight(string(p), "\n"))
		if k == len(p) {
			newlines += k
		} else {
			newlines = k
		}
	}
	return s.w.Write(p)
}

var (
	out    io.Writer = screen{os.Stdout}
	errOut io.Writer = screen{os.Stderr}
)

// gap starts a new paragraph on w: one empty line before it, never two. Writers other than the
// screen (tests) get none.
func gap(w io.Writer) {
	if _, ok := w.(screen); !ok {
		return
	}
	for newlines < 2 {
		fmt.Fprintln(w)
	}
}

// answered records that the user ended their answer with Enter, which the terminal echoed itself.
func answered() { newlines = 1 }

var useColor = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && terminal(os.Stdout) && enableColor()

func paint(code, s string) string {
	if !useColor || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string    { return paint("1", s) }
func dim(s string) string     { return paint("2", s) }
func heading(s string) string { return paint("1;35", s) }

// styleQuestion makes a question stand out: a cyan "?", the question in bold, and its input hint
// (the [ ... ] choices and the ">" where the answer goes, or a line that is only a hint) dim.
func styleQuestion(line string, first bool) string {
	text, hint := line, ""
	if i := strings.LastIndex(line, "["); i >= 0 && strings.HasSuffix(strings.TrimRight(line, " "), ">") {
		text, hint = line[:i], line[i:]
	} else if t := strings.TrimSpace(line); strings.HasPrefix(t, "[") || strings.HasPrefix(t, "(") {
		text, hint = "", line
	}
	if first {
		text = bold(text)
	}
	return text + dim(hint)
}

// settings aligns the "label: value" lines of s into columns, the label dim and the value bold;
// other lines stay as they are. Used for summaries of settings and state (init, whoami, schedule, deps).
func settings(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	type pair struct{ label, value string }
	pairs := make([]*pair, len(lines))
	widest := 0
	for i, l := range lines {
		label, value, ok := strings.Cut(l, ": ")
		if !ok || label == "" || strings.ContainsAny(label, "`.") || width(label) > 28 {
			continue
		}
		pairs[i] = &pair{label, value}
		widest = max(widest, width(label))
	}
	for i, p := range pairs {
		if p != nil {
			lines[i] = dim(p.label) + strings.Repeat(" ", widest-width(p.label)+2) + bold(p.value)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// width is how many terminal columns s takes: Korean, Japanese and Chinese characters take two.
func width(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 ||
			r >= 0xf900 && r <= 0xfaff || r >= 0xff00 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6) {
			n++
		}
	}
	return n
}

// pad fills s with spaces to n columns.
func pad(s string, n int) string { return s + strings.Repeat(" ", max(0, n-width(s))) }
