package main

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const hookMarker = "# gitfolio-managed"

// hookFind locates gitfolio even when a GUI git client runs hooks with a short PATH.
const hookFind = `g=$(command -v gitfolio 2>/dev/null)
for p in /opt/homebrew/bin/gitfolio /usr/local/bin/gitfolio "$HOME/.local/bin/gitfolio" "$HOME/go/bin/gitfolio"; do
	[ -z "$g" ] && [ -x "$p" ] && g=$p
done
`

// Hook scripts never fail and never wait: git keeps working when gitfolio is missing or broken.
// A hook that existed before is renamed to <name>.gitfolio-orig and still runs first;
// for pre-push its exit code is honored, so it can still block a push.
var hookScripts = map[string]string{
	"post-commit": "#!/bin/sh\n" + hookMarker + ` (github.com/Alineteam-Inc/GitFolio-CLI): notes AI agent use for this commit
d=$(dirname "$0")
[ -x "$d/post-commit.gitfolio-orig" ] && "$d/post-commit.gitfolio-orig" "$@"
` + hookFind + `[ -n "$g" ] && "$g" hook post-commit >/dev/null 2>&1
exit 0
`,
	"pre-push": "#!/bin/sh\n" + hookMarker + ` (github.com/Alineteam-Inc/GitFolio-CLI): collects pushed commits in the background
d=$(dirname "$0")
in=$(cat)
if [ -x "$d/pre-push.gitfolio-orig" ]; then
	printf '%s\n' "$in" | "$d/pre-push.gitfolio-orig" "$@" || exit $?
fi
` + hookFind + `[ -n "$g" ] && "$g" hook pre-push "$PPID" >/dev/null 2>&1
exit 0
`,
}

// hooksDir returns the directory git runs repo's hooks from, and whether it is husky's: husky 9 sets
// core.hooksPath to .husky/_, a folder it generates on this computer and keeps out of git. Other
// directories outside .git (a shared core.hooksPath, older husky) are shared with other repositories
// or committed with the project, so they are refused.
func hooksDir(repo string) (dir string, husky bool, err error) {
	out, err := git(repo, "rev-parse", "--git-path", "hooks", "--git-common-dir")
	if err != nil {
		return "", false, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return "", false, fmt.Errorf("unexpected git rev-parse output %q", out)
	}
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(repo, p)
		}
		return filepath.Clean(p)
	}
	hooks, common := abs(lines[0]), abs(lines[1])
	if rel, err := filepath.Rel(common, hooks); err != nil || strings.HasPrefix(rel, "..") {
		if _, err := os.Stat(filepath.Join(hooks, "h")); err == nil && filepath.Base(hooks) == "_" {
			return hooks, true, nil
		}
		return "", false, failure("hooksElsewhere", tildePath(hooks))
	}
	return hooks, false, nil
}

// huskyHooks puts gitfolio's lines at the top of husky's generated pre-push and post-commit files, or
// takes them out. They go first because husky's own line ends the script. husky writes these files
// again on npm install, so scans put the lines back.
func huskyHooks(dir string, install bool) error {
	for name, cmd := range map[string]string{"pre-push": `hook pre-push "$PPID"`, "post-commit": "hook post-commit"} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		s := string(b)
		if i := strings.Index(s, hookMarker); i >= 0 {
			if install {
				continue // already there
			}
			if j := strings.Index(s, hookMarker+" end\n"); j > i {
				s = s[:i] + s[j+len(hookMarker+" end\n"):]
			}
		} else if install {
			first, rest, _ := strings.Cut(cmp.Or(s, "#!/usr/bin/env sh\n"), "\n")
			s = first + "\n" + hookMarker + " (github.com/Alineteam-Inc/GitFolio-CLI)\n" + hookFind +
				`[ -n "$g" ] && "$g" ` + cmd + " </dev/null >/dev/null 2>&1\n" + hookMarker + " end\n" + rest
		} else {
			continue
		}
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func installHooks(repo string) error {
	dir, husky, err := hooksDir(repo)
	if err != nil {
		return err
	}
	if husky {
		return huskyHooks(dir, true)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, script := range hookScripts {
		p := filepath.Join(dir, name)
		if b, err := os.ReadFile(p); err == nil {
			if strings.Contains(string(b), hookMarker) {
				continue // already installed
			}
			if _, err := os.Stat(p + ".gitfolio-orig"); err == nil {
				return failure("hooksBothExist", tildePath(p))
			}
			if err := os.Rename(p, p+".gitfolio-orig"); err != nil {
				return err
			}
		}
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// uninstallHooks removes gitfolio's hooks and puts back the ones that were there before.
func uninstallHooks(repo string) error {
	dir, husky, err := hooksDir(repo)
	if err != nil {
		return err
	}
	if husky {
		return huskyHooks(dir, false)
	}
	for name := range hookScripts {
		p := filepath.Join(dir, name)
		if b, err := os.ReadFile(p); err != nil || !strings.Contains(string(b), hookMarker) {
			continue
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		if _, err := os.Stat(p + ".gitfolio-orig"); err == nil {
			if err := os.Rename(p+".gitfolio-orig", p); err != nil {
				return err
			}
		}
	}
	return nil
}

// hookStatus reports whether gitfolio's pre-push hook is installed in repo, for `gitfolio list`.
func hookStatus(repo string) string {
	dir, _, err := hooksDir(repo)
	if err != nil {
		return "manual"
	}
	if b, err := os.ReadFile(filepath.Join(dir, "pre-push")); err == nil && strings.Contains(string(b), hookMarker) {
		return "on"
	}
	return "off"
}

// cmdHook runs inside git hooks. Hooks ignore its exit code, so errors only matter for debugging.
func cmdHook(dir string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: gitfolio hook post-commit|pre-push")
	}
	switch args[0] {
	case "post-commit":
		agents := agentsFromEnv(os.Getenv)
		if len(agents) == 0 {
			return nil
		}
		out, err := git(".", "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		return appendAgentTag(dir, agentTag{Hash: strings.TrimSpace(out), Agents: agents})
	case "pre-push":
		// git waits for this hook, so the work runs in a separate process that outlives it.
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		arg := ""
		if len(args) > 1 {
			arg = args[1]
		}
		wait := strconv.Itoa(pushPID(arg))
		if wait == "0" {
			// The push process can't be found: Git for Windows runs husky's `#!/usr/bin/env sh` stubs
			// through env, whose exec leaves no parent link. Wait for the push to update the refs instead.
			wait = "refs:" + remoteRefs(".")
		}
		noticeUpdate(dir)
		return exec.Command(exe, "hook", "push-wait", wait).Start()
	case "push-wait":
		signal.Ignore(syscall.SIGHUP) // keep going if the terminal closes right after the push
		if len(args) > 1 {
			if refs, ok := strings.CutPrefix(args[1], "refs:"); ok {
				waitRefs(refs, 10*time.Minute)
			} else if pid, err := strconv.Atoi(args[1]); err == nil {
				waitExit(pid, 10*time.Minute)
			}
		}
		top, err := topLevel(".")
		if err != nil {
			return err
		}
		if updatesItself() { // what the next push tells about a newer release (noticeUpdate), outside the lock
			defer lookUpLatest(dir)
		}
		// Only commits on remote-tracking refs are sent (pending), so a rejected push adds nothing.
		return withLock(dir, func() error {
			repos, err := loadRepos(dir)
			if err != nil {
				return err
			}
			i := slices.IndexFunc(repos, func(r Repo) bool { return r.Path == top })
			if i < 0 {
				return nil
			}
			if _, err := scanRepo(dir, &repos[i], false); err != nil {
				return err
			}
			if err := saveRepos(dir, repos); err != nil {
				return err
			}
			cfg, err := loadConfig(dir)
			if err != nil || cfg.AutoSyncOff {
				return err
			}
			_, err = syncData(dir, false) // offline or failed: sent by the next push or sync
			return err
		})
	}
	return fmt.Errorf("unknown hook %q", args[0])
}

// remoteRefs fingerprints the repository's remote-tracking refs, which git updates when a push succeeds.
func remoteRefs(repo string) string {
	out, _ := git(repo, "for-each-ref", "--format=%(objectname) %(refname)", "refs/remotes")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(out)))[:16]
}

// waitRefs waits until the remote-tracking refs differ from before and have stopped changing, at most
// max. A push that is rejected or updates no remote-tracking ref waits the whole time; the scan after
// it then finds nothing new.
func waitRefs(before string, max time.Duration) {
	last := before
	for deadline := time.Now().Add(max); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		now := remoteRefs(".")
		if now != before && now == last {
			return
		}
		last = now
	}
}

// withLock runs fn while holding gitfolio's data lock, so hooks and commands never write at the same time.
// ponytail: a lock file instead of OS file locks (none portable in the standard library). It holds the
// process ID of its run, so a lock left behind by a run that crashed is taken over at once, while a run
// that is slow or was asleep with the computer keeps it for up to an hour.
func withLock(dir string, fn func() error) error {
	p := filepath.Join(dir, "lock")
	me := []byte(strconv.Itoa(os.Getpid()))
	deadline := time.Now().Add(30 * time.Second)
	for !lockTurn.TryLock() {
		if time.Now().After(deadline) {
			return failure("busy")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer lockTurn.Unlock()
	for ; ; time.Sleep(20 * time.Millisecond) {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = f.Write(me)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				os.Remove(p)
				return err
			}
			defer release(p, me)
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		b, err := os.ReadFile(p)
		st, serr := os.Stat(p)
		if err == nil && serr == nil && leftBehind(b, time.Since(st.ModTime())) {
			takeOver(p, b, st.ModTime())
			continue
		}
		if time.Now().After(deadline) {
			return failure("busy")
		}
	}
}

// lockTurn lets one withLock of this process at a time go for the lock file, so a lock file with this
// process's ID that it meets is one this process could not remove.
var lockTurn sync.Mutex

// release removes the lock while it is still this run's, not taken over after an hour. Windows refuses
// while a waiting run reads the file: it tries again for a second, and a lock still there is taken over
// by this process's next run, or by any once this process has ended.
func release(p string, me []byte) {
	for range 50 {
		if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, me) || os.Remove(p) == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// leftBehind reports whether a lock with content b and of this age outlived its run: the process it
// names is this one (see lockTurn) or gone, or it is an hour old (the ID reused by another program).
// Releases before this one leave it empty, as a run does for a moment while writing it; theirs count
// after 10 minutes, as there.
func leftBehind(b []byte, age time.Duration) bool {
	pid, err := strconv.Atoi(string(b))
	if err != nil || pid <= 0 {
		return age > 10*time.Minute
	}
	return pid == os.Getpid() || age > time.Hour || !alive(pid)
}

// takeOver removes the lock left behind as seen (content and time), unless it changed in the meantime.
// One waiter at a time does it, holding lock.takeover: two that saw the same lock left behind would
// otherwise both remove it, the second one the new lock the first had just made.
func takeOver(p string, seen []byte, at time.Time) {
	t := p + ".takeover"
	f, err := os.OpenFile(t, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if st, err := os.Stat(t); err == nil && time.Since(st.ModTime()) > time.Minute {
			os.Remove(t) // its waiter stopped halfway: a takeover takes microseconds
		}
		return
	}
	f.Close()
	defer os.Remove(t)
	b, err := os.ReadFile(p)
	st, serr := os.Stat(p)
	if err == nil && serr == nil && bytes.Equal(b, seen) && st.ModTime().Equal(at) {
		os.Remove(p)
	}
}
