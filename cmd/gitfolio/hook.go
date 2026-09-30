package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
	"post-commit": "#!/bin/sh\n" + hookMarker + ` (github.com/Alineteam-Inc/GitFolio): notes AI agent use for this commit
d=$(dirname "$0")
[ -x "$d/post-commit.gitfolio-orig" ] && "$d/post-commit.gitfolio-orig" "$@"
` + hookFind + `[ -n "$g" ] && "$g" hook post-commit >/dev/null 2>&1
exit 0
`,
	"pre-push": "#!/bin/sh\n" + hookMarker + ` (github.com/Alineteam-Inc/GitFolio): collects pushed commits in the background
d=$(dirname "$0")
in=$(cat)
if [ -x "$d/pre-push.gitfolio-orig" ]; then
	printf '%s\n' "$in" | "$d/pre-push.gitfolio-orig" "$@" || exit $?
fi
` + hookFind + `[ -n "$g" ] && "$g" hook pre-push "$PPID" >/dev/null 2>&1
exit 0
`,
}

// hooksDir returns the repository's own hooks directory. Directories outside .git (core.hooksPath,
// husky) are shared with other repositories or committed with the project, so they are refused.
func hooksDir(repo string) (string, error) {
	out, err := git(repo, "rev-parse", "--git-path", "hooks", "--git-common-dir")
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return "", fmt.Errorf("unexpected git rev-parse output %q", out)
	}
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(repo, p)
		}
		return filepath.Clean(p)
	}
	hooks, common := abs(lines[0]), abs(lines[1])
	if rel, err := filepath.Rel(common, hooks); err != nil || strings.HasPrefix(rel, "..") {
		return "", failure("hooksElsewhere", tildePath(hooks))
	}
	return hooks, nil
}

func installHooks(repo string) error {
	dir, err := hooksDir(repo)
	if err != nil {
		return err
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
	dir, err := hooksDir(repo)
	if err != nil {
		return err
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
	dir, err := hooksDir(repo)
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
		pid := ""
		if len(args) > 1 {
			pid = args[1]
		}
		return exec.Command(exe, "hook", "push-wait", strconv.Itoa(pushPID(pid))).Start()
	case "push-wait":
		signal.Ignore(syscall.SIGHUP) // keep going if the terminal closes right after the push
		if len(args) > 1 {
			if pid, err := strconv.Atoi(args[1]); err == nil {
				waitExit(pid, 10*time.Minute)
			}
		}
		top, err := topLevel(".")
		if err != nil {
			return err
		}
		// scanRepo only reads commits reachable from remote-tracking refs, so a rejected push adds nothing.
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

// withLock runs fn while holding gitfolio's data lock, so hooks and commands never write at the same time.
// ponytail: lock file instead of OS file locks (none portable in the standard library); a lock older
// than 10 minutes is treated as left behind by a crashed run.
func withLock(dir string, fn func() error) error {
	p := filepath.Join(dir, "lock")
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			defer os.Remove(p)
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) > 10*time.Minute {
			os.Remove(p)
			continue
		}
		if time.Now().After(deadline) {
			return failure("busy")
		}
	}
}
