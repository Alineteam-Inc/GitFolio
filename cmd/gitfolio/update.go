package main

import (
	"errors"
	"fmt"
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

// checkUpdate looks for a newer release at most once a day and offers to install it the way this copy
// was installed (Homebrew, install.sh or install.ps1). It reports whether it updated, so the caller
// stops and the user runs the command again with the new binary. Builds from source never check, and
// nothing is asked unless a person is at the terminal (not hooks, scheduled runs or redirected output).
func checkUpdate(dir string) bool {
	if version == "dev" || !interactive() || !terminal(os.Stdout) {
		return false
	}
	p := filepath.Join(dir, updateFile)
	var st struct {
		CheckedAt time.Time `json:"checkedAt"`
	}
	if loadJSON(p, &st) == nil && time.Since(st.CheckedAt) < updateEvery {
		return false
	}
	st.CheckedAt = time.Now()
	_ = saveJSON(p, st) // offline or not, the next check is a day away
	latest, err := latestRelease()
	if err != nil || !newer(latest, version) {
		return false
	}
	lang := detectLang(os.Getenv)
	a := strings.ToLower(prompt(fmt.Sprintf(tr(lang, "updateAsk"), latest, version)))
	if stdinClosed || (a != "" && a != "y" && a != "yes") {
		return false
	}
	cmd, manual := updateCommand(latest)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		warn(lang, "updateFailed", err, manual)
		return false
	}
	say(lang, "updated", latest)
	return true
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
