package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// latestURL redirects to the newest release's tag page (no API rate limit).
var latestURL = "https://github.com/Alineteam-Inc/GitFolio/releases/latest"

const (
	updateFile  = "update.json"
	installSh   = "https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh"
	installPs1  = "https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.ps1"
	updateEvery = 24 * time.Hour
)

// updateState is update.json: the newest release as last looked up, and when the user was last asked
// about it at a terminal or told about it during a push.
type updateState struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest,omitempty"` // the newest release then, e.g. "v0.5.0"
	AskedAt   time.Time `json:"askedAt,omitempty"`
	NoticedAt time.Time `json:"noticedAt,omitempty"`
}

// lookUpLatest looks up the newest release at most once a day and keeps what it found. Offline or not,
// the next look is a day away.
func lookUpLatest(dir string) updateState {
	p := filepath.Join(dir, updateFile)
	var st updateState
	if loadJSON(p, &st) == nil && time.Since(st.CheckedAt) < updateEvery {
		return st
	}
	st.CheckedAt = time.Now()
	if latest, err := latestRelease(); err == nil {
		st.Latest = latest
	}
	_ = saveJSON(p, st)
	return st
}

// checkUpdate offers, at most once a day, to install a newer release the way this copy was installed
// (Homebrew, install.sh or install.ps1). It reports whether it updated, so the caller stops and the user
// runs the command again with the new binary. Builds from source never check, and nothing is asked
// unless a person is at the terminal (not hooks, scheduled runs or redirected output).
func checkUpdate(dir string) bool {
	if version == "dev" || !interactive() || !terminal(os.Stdout) {
		return false
	}
	st := lookUpLatest(dir)
	if !newer(st.Latest, version) || time.Since(st.AskedAt) < updateEvery {
		return false
	}
	st.AskedAt = time.Now()
	_ = saveJSON(filepath.Join(dir, updateFile), st)
	latest := st.Latest
	lang := detectLang(os.Getenv)
	a := strings.ToLower(prompt(fmt.Sprintf(tr(lang, "updateAsk"), latest, version)))
	if stdinClosed || (a != "" && a != "y" && a != "yes") {
		return false
	}
	if err := installUpdate(latest); err != nil {
		warn(lang, "failed", err)
		return false
	}
	say(lang, "updated", latest)
	return true
}

// ttyOut opens the terminal git runs in. A hook's own output goes nowhere (the hook script discards it),
// and a GUI git client has no terminal: then it fails and nothing is shown.
var ttyOut = func() (io.WriteCloser, error) { return openTTY() }

// noticeUpdate tells the person pushing, at most once a day, that a newer release is out: someone who
// only pushes never sees the question an interactive command asks. It reads what an earlier look found,
// so a push never waits on the network (the push's background sync looks it up, lookUpLatest).
func noticeUpdate(dir string) {
	p := filepath.Join(dir, updateFile)
	var st updateState
	if version == "dev" || loadJSON(p, &st) != nil || !newer(st.Latest, version) || time.Since(st.NoticedAt) < updateEvery {
		return
	}
	w, err := ttyOut()
	if err != nil {
		return // no terminal: the next push tries again
	}
	defer w.Close()
	fmt.Fprint(w, statusPrefix+fmt.Sprintf(tr(detectLang(os.Getenv), "updateNotice"), strings.TrimPrefix(st.Latest, "v"), version))
	st.NoticedAt = time.Now()
	_ = saveJSON(p, st)
}

// installUpdate installs release tag latest the way this copy was installed.
func installUpdate(latest string) error {
	cmd, manual := updateCommand(latest)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return failure("updateFailed", err, manual)
	}
	return nil
}

// cmdUpdate checks for a newer release now, whenever the daily check last ran, and installs it.
func cmdUpdate(dir string) error {
	lang := detectLang(os.Getenv)
	if version == "dev" {
		say(lang, "updateFromSource")
		return nil
	}
	latest, err := latestRelease()
	if err != nil {
		return failure("updateCheckFailed", err)
	}
	now := time.Now() // looked up and asked: no offer again today
	_ = saveJSON(filepath.Join(dir, updateFile), updateState{CheckedAt: now, Latest: latest, AskedAt: now})
	if !newer(latest, version) {
		say(lang, "updateLatest", version)
		return nil
	}
	say(lang, "updating", version, latest)
	if err := installUpdate(latest); err != nil {
		return err
	}
	say(lang, "updatedNow", latest)
	return nil
}

// latestRelease returns the newest release tag, e.g. "v0.2.0".
func latestRelease() (string, error) {
	c := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := c.Head(latestURL)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	tag := path.Base(res.Header.Get("Location")) // …/releases/tag/v0.2.0
	if !strings.HasPrefix(tag, "v") {
		return "", errors.New("no release")
	}
	return tag, nil
}

// newer reports whether release tag a ("v1.2.3") is newer than version b ("1.2.3"). Anything that is
// not three numbers counts as not newer.
func newer(a, b string) bool {
	parse := func(s string) (n [3]int, ok bool) {
		parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
		if len(parts) != 3 {
			return n, false
		}
		for i, p := range parts {
			v, err := strconv.Atoi(p)
			if err != nil {
				return n, false
			}
			n[i] = v
		}
		return n, true
	}
	x, ok1 := parse(a)
	y, ok2 := parse(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// updateCommand returns the command that installs release tag the way this binary was installed, and
// the same thing written out for the user in case it fails.
func updateCommand(tag string) (*exec.Cmd, string) {
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
	}
	sep := string(filepath.Separator)
	switch {
	case strings.Contains(exe, sep+"Caskroom"+sep): // Homebrew cask, macOS or Linux
		s := "brew update && brew upgrade --cask gitfolio"
		return exec.Command("sh", "-c", s), s
	case runtime.GOOS == "windows":
		s := "irm " + installPs1 + " | iex"
		c := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", s)
		c.Env = append(os.Environ(), "GITFOLIO_VERSION="+tag, "GITFOLIO_INSTALL_DIR="+filepath.Dir(exe))
		return c, s
	default:
		s := "curl -fsSL " + installSh + " | sh"
		c := exec.Command("sh", "-c", s)
		c.Env = append(os.Environ(), "GITFOLIO_VERSION="+tag, "GITFOLIO_INSTALL_DIR="+filepath.Dir(exe))
		return c, s
	}
}

// terminal reports whether f is a terminal: a character device other than the null device, which is
// one too (`gitfolio email add x </dev/null` must not ask for a code).
func terminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(st, null)
}
