package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

// logFormat separates commits with \x1e and fields with \x1f; numstat lines follow the last \x1f.
const logFormat = "--format=%x1e%H%x1f%aI%x1f%aE%x1f%B%x1f"

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
		return "", fmt.Errorf("%s is not a git repository", dir)
	}
	return strings.TrimSpace(out), nil
}

// myEmails returns the lowercased emails that identify the user's own commits in repo.
func myEmails(repo string) (map[string]bool, error) {
	out, err := git(repo, "config", "user.email")
	email := strings.ToLower(strings.TrimSpace(out))
	if err != nil || email == "" {
		return nil, fmt.Errorf("user.email is not set (git config --global user.email you@example.com)")
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

func gitLog(repo string, rng []string) (string, error) {
	args := []string{"-c", "core.quotePath=false", "log", "--no-merges", "--no-color", "--no-show-signature", "-M", "--numstat", logFormat}
	return git(repo, append(args, rng...)...)
}

func parseLog(out string) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.SplitN(rec, "\x1f", 5)
		if len(f) < 5 {
			continue
		}
		c := Commit{Hash: f[0], Date: f[1], Email: f[2], Message: strings.TrimSpace(f[3])}
		for _, line := range strings.Split(f[4], "\n") {
			p := strings.SplitN(line, "\t", 3)
			if len(p) < 3 {
				continue
			}
			add, _ := strconv.Atoi(p[0]) // binary files report "-", counted as 0
			del, _ := strconv.Atoi(p[1])
			c.Files = append(c.Files, FileStat{Name: baseName(p[2]), Add: add, Del: del})
		}
		commits = append(commits, c)
	}
	return commits
}

// baseName returns the file name of a numstat path, taking the new side of a rename
// ("dir/{old => new}/f.go" or "old.go => new.go").
func baseName(p string) string {
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
	return path.Base(p)
}
