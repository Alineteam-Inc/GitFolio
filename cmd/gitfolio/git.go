package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
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
	"dev.azure.com":     "AZURE_DEVOPS",
	"ssh.dev.azure.com": "AZURE_DEVOPS",
}

// remote returns the repository's provider and owner/repo on its git service (origin first,
// otherwise the first remote). aline.team resolves them to the service's repository ID.
// Both are empty without such a remote.
func remote(repo string) (provider, namespace string) {
	out, err := git(repo, "remote", "get-url", "origin")
	if err != nil {
		names, err := git(repo, "remote")
		if err != nil || len(strings.Fields(names)) == 0 {
			return "", ""
		}
		if out, err = git(repo, "remote", "get-url", strings.Fields(names)[0]); err != nil {
			return "", ""
		}
	}
	return parseRemote(strings.TrimSpace(out))
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
	provider = providers[strings.ToLower(host)]
	if provider == "" {
		provider = "OTHER"
	}
	return provider, p
}

// myEmails returns the lowercased emails that identify the user's own commits in repo.
func myEmails(repo string) (map[string]bool, error) {
	out, err := git(repo, "config", "user.email")
	email := strings.ToLower(strings.TrimSpace(out))
	if err != nil || email == "" {
		return nil, failure("noUserEmail")
	}
	// Verified work emails from aline.team are added here in ROADMAP step 7.
	return map[string]bool{email: true}, nil
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
	args := []string{"-c", "core.quotePath=false", "log", "--no-merges", "--no-color", "--no-show-signature", "-M", "--numstat", logFormat}
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
		for _, line := range strings.Split(f[5], "\n") {
			p := strings.SplitN(line, "\t", 3)
			if len(p) < 3 {
				continue
			}
			add, _ := strconv.Atoi(p[0]) // binary files report "-", counted as 0
			del, _ := strconv.Atoi(p[1])
			full := newPath(p[2])
			c.Files = append(c.Files, FileStat{Name: path.Base(full), Add: add, Del: del, path: full})
		}
		commits = append(commits, c)
	}
	return commits
}

// baseName returns the file name of a numstat path.
func baseName(p string) string { return path.Base(newPath(p)) }

// newPath returns the path of a numstat entry, taking the new side of a rename
// ("dir/{old => new}/f.go" or "old.go => new.go").
func newPath(p string) string {
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			p = u
		}
	}
	if i := strings.Index(p, " => "); i >= 0 {
		l, r := strings.LastIndex(p[:i], "{"), strings.Index(p[i:], "}")
		if l >= 0 && r >= 0 {
			p = p[:l] + p[i+4:i+r] + p[i+r+1:]
		} else {
			p = p[i+4:]
		}
	}
	return p
}
