package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// logFormat separates commits with \x1e and fields with \x1f; numstat lines follow the last \x1f.
const logFormat = "--format=%x1e%H%x1f%aI%x1f%aN%x1f%aE%x1f%B%x1f"

// git runs git inside dir. GIT_DIR and friends are dropped so that running
// from inside a git hook does not redirect the command to the hook's repository.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") && !strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, traceEnv()...) // git command history (history.go)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// gitWorks reports whether git can run, and puts it on this run's PATH when it is installed outside
// it (gitDirs). On macOS /usr/bin/git is there without git too: it stands in for the Command Line Tools,
// and only counts once they are installed. Asking xcode-select, not running it, so Apple's install
// window does not open by itself.
func gitWorks() bool {
	if p, err := exec.LookPath("git"); err == nil &&
		(runtime.GOOS != "darwin" || p != "/usr/bin/git" || exec.Command("xcode-select", "-p").Run() == nil) {
		return true
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, d := range gitDirs() {
		if st, err := os.Stat(filepath.Join(d, name)); err == nil && !st.IsDir() {
			return os.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH")) == nil
		}
	}
	return false
}

// gitDirs are where git is installed but may be off the PATH: an app started from Finder gets only
// /usr/bin:/bin:/usr/sbin:/sbin, and a terminal open while git was installed on Windows has the old PATH.
var gitDirs = func() []string {
	if runtime.GOOS == "windows" {
		return []string{filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd")}
	}
	return []string{"/opt/homebrew/bin", "/usr/local/bin"}
}

// gitDownload is git's own download page for this system, for installing it by hand.
func gitDownload() string {
	switch runtime.GOOS {
	case "darwin":
		return "https://git-scm.com/download/mac"
	case "windows":
		return "https://git-scm.com/download/win"
	}
	return "https://git-scm.com/download/linux"
}

// cmdGit tells whether git is there (path and version), or with install starts installing it: at the
// terminal as init does, after asking; for a program (--json) without asking, the way it can start
// without a terminal (gitInstallStart).
func cmdGit(args []string) error {
	jsonMode(args)
	args = withoutJSON(args)
	lang := detectLang(os.Getenv)
	switch {
	case len(args) == 0:
		found := gitWorks()
		path, version := "", ""
		if found {
			path, _ = exec.LookPath("git")
			if out, err := exec.Command("git", "--version").Output(); err == nil { // "git version 2.47.0"
				version = strings.TrimPrefix(strings.TrimSpace(string(out)), "git version ")
			}
		}
		if jsonOut {
			return printJSON(map[string]any{"found": found, "path": path, "version": version})
		}
		if !found {
			say(lang, "gitMissing")
			return nil
		}
		say(lang, "gitFound", version, path)
		return nil
	case len(args) == 1 && args[0] == "install":
		if gitWorks() {
			if jsonOut {
				return printJSON(map[string]any{"started": false, "how": "installed"})
			}
			say(lang, "gitInstalled")
			return nil
		}
		if jsonOut {
			return gitInstallStart(lang)
		}
		if !interactive() || !installGit(lang) {
			return failure("noGit")
		}
		return nil
	}
	return failure("usage", "gitfolio git [install] [--json]")
}

// gitInstallStart starts installing git for a program, asking nothing: Apple's install window on macOS
// (it returns at once; the program checks again once the user is done), winget on Windows (it waits for
// the install and may ask for administrator rights). Where that needs a terminal for sudo or there is
// no installer, it gives the manual way: a message and git's download page.
func gitInstallStart(lang string) error {
	var args []string
	how := "manual"
	switch runtime.GOOS {
	case "darwin":
		args, how = []string{"xcode-select", "--install"}, "xcodeSelect"
	case "windows":
		if args = gitInstaller("windows", func(n string) bool { _, err := exec.LookPath(n); return err == nil }, false); args != nil {
			args, how = append(args, "--accept-package-agreements", "--accept-source-agreements"), "winget"
		}
	}
	if args == nil {
		return printJSON(map[string]any{"started": false, "how": how, "message": strings.TrimSpace(tr(lang, "gitInstallManual")), "url": gitDownload()})
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = errOut, errOut // stdout carries the result alone
	if err := cmd.Run(); err != nil {
		return failure("gitInstallFailed", err)
	}
	return printJSON(map[string]any{"started": true, "how": how, "url": gitDownload()})
}

// gitInstaller is the command that installs git with the system's own installer, nil when there is
// none to use: the Command Line Tools on macOS, winget on Windows, the package manager on Linux (through
// sudo unless root). has reports whether a command is on the PATH.
func gitInstaller(goos string, has func(string) bool, root bool) []string {
	switch goos {
	case "darwin":
		return []string{"xcode-select", "--install"}
	case "windows":
		if has("winget") {
			return []string{"winget", "install", "--id", "Git.Git", "-e", "--source", "winget"}
		}
		return nil
	}
	var install []string
	switch {
	case has("apt-get"):
		install = []string{"sh", "-c", "apt-get update && apt-get install -y git"}
	case has("dnf"):
		install = []string{"dnf", "install", "-y", "git"}
	case has("yum"):
		install = []string{"yum", "install", "-y", "git"}
	case has("pacman"):
		install = []string{"pacman", "-S", "--noconfirm", "git"}
	case has("zypper"):
		install = []string{"zypper", "--non-interactive", "install", "git"}
	case has("apk"):
		install = []string{"apk", "add", "git"}
	default:
		return nil
	}
	if root {
		return install
	}
	if !has("sudo") {
		return nil
	}
	return append([]string{"sudo"}, install...)
}

// installGit offers, at the terminal, to install git with gitInstaller, and reports whether git runs
// after it. Nothing is installed without a yes.
func installGit(lang string) bool {
	warn(lang, "gitMissing")
	args := gitInstaller(runtime.GOOS, func(name string) bool { _, err := exec.LookPath(name); return err == nil }, os.Geteuid() == 0)
	if args == nil {
		notice(tr(lang, "gitInstallManual"))
		return false
	}
	shown := make([]string, len(args))
	for i, a := range args {
		if shown[i] = a; strings.ContainsAny(a, " &") {
			shown[i] = shellQuote(a)
		}
	}
	if !askYesNo(lang, "gitInstallAsk", true, strings.Join(shown, " ")) || stdinClosed {
		return false
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		warn(lang, "gitInstallFailed", err)
		notice(tr(lang, "gitInstallManual"))
		return false
	}
	if !gitWorks() { // macOS installs in its own window, after this returns
		notice(tr(lang, "gitInstallPending"))
		return false
	}
	say(lang, "gitInstalled")
	return true
}

func topLevel(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if abs, aerr := filepath.Abs(dir); aerr == nil {
			dir = abs
		}
		return "", failure("notGitRepo", tildePath(dir))
	}
	return strings.TrimSpace(out), nil
}

// providers maps git service hosts to the provider sent to aline.team; any other host is OTHER
// (self-hosted servers included), so internal host names never leave the machine.
var providers = map[string]string{
	"github.com":        "GITHUB",
	"ssh.github.com":    "GITHUB",
	"gitlab.com":        "GITLAB",
	"bitbucket.org":     "BITBUCKET",
	"dev.azure.com":     "DEVOPS",
	"ssh.dev.azure.com": "DEVOPS",
}

// remote returns the repository's provider and owner/repo on its git service (origin first,
// otherwise the first remote). aline.team resolves them to the service's repository ID.
// It is where commits are pushed: the push URL when it differs from the fetch URL (fetch from GitLab,
// push to GitHub), the first of several, unless that is not a git service (a local mirror): then the
// fetch URL. Both are empty without such a remote.
func remote(repo string) (provider, namespace string) {
	name := "origin"
	if _, err := git(repo, "remote", "get-url", name); err != nil {
		names, err := git(repo, "remote")
		if err != nil || len(strings.Fields(names)) == 0 {
			return "", ""
		}
		name = strings.Fields(names)[0]
	}
	for _, args := range [][]string{{"remote", "get-url", "--push", name}, {"remote", "get-url", name}} {
		if out, err := git(repo, args...); err == nil {
			first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
			if provider, namespace = parseRemote(strings.TrimSpace(first)); provider != "" {
				return provider, namespace
			}
		}
	}
	return "", ""
}

// parseRemote extracts the provider and owner/repo from https, ssh and scp-style
// ("git@host:owner/repo.git") remote URLs. Host, credentials (https://user:token@host/...)
// and port are never returned.
func parseRemote(raw string) (provider, namespace string) {
	var host, p string
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host, p = u.Hostname(), u.Path
	} else if !strings.Contains(raw, "://") {
		if i := strings.Index(raw, ":"); i > 0 {
			host, p = raw[:i], raw[i+1:]
			if j := strings.LastIndex(host, "@"); j >= 0 {
				host = host[j+1:]
			}
		}
	}
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	if !strings.Contains(host, ".") || p == "" { // local paths such as /srv/repo.git or C:\repo
		return "", ""
	}
	host = strings.ToLower(host)
	provider = providers[host]
	if strings.HasSuffix(host, ".visualstudio.com") {
		provider = "DEVOPS"
	}
	if provider == "" {
		provider = "OTHER"
	}
	if provider == "DEVOPS" {
		p = devopsNamespace(host, p)
	}
	return provider, p
}

// devopsNamespace turns an Azure DevOps remote path into organization/project/repository, the form
// aline.team builds https://dev.azure.com/org/project/_git/repo from. It takes dev.azure.com/org/project/_git/repo,
// ssh.dev.azure.com:v3/org/project/repo and the older org.visualstudio.com/[DefaultCollection/]project/_git/repo.
func devopsNamespace(host, p string) string {
	if u, err := url.PathUnescape(p); err == nil { // scp-style paths are still escaped ("My%20Project")
		p = u
	}
	parts := strings.Split(strings.TrimPrefix(p, "v3/"), "/")
	if org, ok := strings.CutSuffix(host, ".visualstudio.com"); ok && org != "vs-ssh" {
		parts = append([]string{org}, parts...)
		if len(parts) > 1 && parts[1] == "DefaultCollection" {
			parts = slices.Delete(parts, 1, 2)
		}
	}
	if i := slices.Index(parts, "_git"); i == 1 && len(parts) == 3 {
		parts[1] = parts[2] // org/_git/repo: the project has the repository's name
	} else if i >= 0 {
		parts = slices.Delete(parts, i, i+1)
	}
	return strings.Join(parts, "/")
}

// myEmails returns the lowercased emails that identify the user's own commits in repo:
// the work emails registered with `gitfolio email` and the repository's git config user.email.
func myEmails(repo string, work []string) (map[string]bool, error) {
	mine := map[string]bool{}
	for _, e := range work {
		mine[strings.ToLower(e)] = true
	}
	if out, err := git(repo, "config", "user.email"); err == nil {
		if e := strings.ToLower(strings.TrimSpace(out)); e != "" {
			mine[e] = true
		}
	}
	if len(mine) == 0 {
		return nil, failure("noUserEmail")
	}
	return mine, nil
}

// logRange picks what to read: pushed commits when the repository has remotes, otherwise HEAD.
// It returns nil when the repository has no commits yet.
func logRange(repo string) ([]string, error) {
	if _, err := git(repo, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return nil, nil
	}
	out, err := git(repo, "for-each-ref", "--count=1", "refs/remotes")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) != "" {
		return []string{"--remotes"}, nil
	}
	return []string{"HEAD"}, nil
}

// remoteBranches maps every commit on a remote-tracking branch to one branch name ("main",
// "feature/login"). The remote's default branch comes first, so a commit already merged there is
// reported on it, as the aline.team GitHub sync sees it; other commits get the branch they were pushed to.
func remoteBranches(repo string) (map[string]string, error) {
	out, err := git(repo, "for-each-ref", "--format=%(refname)", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, ref := range strings.Fields(out) {
		if !strings.HasSuffix(ref, "/HEAD") {
			refs = append(refs, ref)
		}
	}
	if head, err := git(repo, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil {
		if i := slices.Index(refs, strings.TrimSpace(head)); i > 0 {
			refs = append(append([]string{refs[i]}, refs[:i]...), refs[i+1:]...)
		}
	}
	branch := map[string]string{}
	var done []string
	for _, ref := range refs {
		out, err := git(repo, append([]string{"rev-list", "--no-merges", ref, "--not"}, done...)...)
		if err != nil {
			return nil, err
		}
		_, name, _ := strings.Cut(strings.TrimPrefix(ref, "refs/remotes/"), "/") // drop the remote name
		for _, h := range strings.Fields(out) {
			branch[h] = name
		}
		done = append(done, ref)
	}
	return branch, nil
}

func gitLog(repo string, rng []string) (string, error) {
	args := []string{"-c", "core.quotePath=false", "log", "--no-merges", "--no-color", "--no-show-signature", "-M", "--numstat", "--summary", logFormat}
	return git(repo, append(args, rng...)...)
}

func parseLog(out string) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.SplitN(rec, "\x1f", 6)
		if len(f) < 6 {
			continue
		}
		c := Commit{Hash: f[0], Date: f[1], AuthorEmail: f[3], Message: strings.TrimSpace(f[4])}
		c.AIAgents = detectAgents(f[2], c.AuthorEmail, c.Message)
		c.coAuthors = coAuthorEmails(c.Message)
		created := map[string]bool{}
		for _, line := range strings.Split(f[5], "\n") {
			if rest, ok := strings.CutPrefix(line, " create mode "); ok { // --summary: " create mode 100644 <path>"
				if _, p, ok := strings.Cut(rest, " "); ok {
					created[newPath(p)] = true
				}
				continue
			}
			p := strings.SplitN(line, "\t", 3)
			if len(p) < 3 {
				continue
			}
			add, _ := strconv.Atoi(p[0]) // binary files report "-", counted as 0
			del, _ := strconv.Atoi(p[1])
			c.Files = append(c.Files, FileStat{Name: newPath(p[2]), Add: add, Del: del})
		}
		for i := range c.Files { // --summary lines follow the numstat lines
			c.Files[i].Created = created[c.Files[i].Name]
		}
		commits = append(commits, c)
	}
	return commits
}

// newPath returns the repository-relative path of a numstat entry as git shows it, taking the new side of
// a rename ("dir/{old => new}/f.go", "{src => }/f.go" or "old.go => new.go").
func newPath(p string) string {
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			p = u
		}
	}
	if i := strings.Index(p, " => "); i >= 0 {
		l, r := strings.LastIndex(p[:i], "{"), strings.Index(p[i:], "}")
		if l >= 0 && r >= 0 {
			p = strings.TrimPrefix(strings.ReplaceAll(p[:l]+p[i+4:i+r]+p[i+r+1:], "//", "/"), "/")
		} else {
			p = p[i+4:]
		}
	}
	return p
}
