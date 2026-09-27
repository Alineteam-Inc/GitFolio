package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Repo struct {
	ID        string `json:"id"`
	Path      string `json:"path"` // local only, never exported or sent
	Name      string `json:"name"`
	Provider  string `json:"provider,omitempty"`  // GITHUB, GITLAB, ... or OTHER
	Namespace string `json:"namespace,omitempty"` // owner/repo on the git service
	LastScan  string `json:"last_scan,omitempty"`
}

type FileStat struct {
	Name string `json:"name"`
	Add  int    `json:"add"`
	Del  int    `json:"del"`
}

type Commit struct {
	Repo          string     `json:"repo"`                // Repo.ID locally, repository name in export
	Provider      string     `json:"provider,omitempty"`  // filled in export only
	Namespace     string     `json:"namespace,omitempty"` // filled in export only
	Hash          string     `json:"hash"`
	AuthorEmail   string     `json:"authorEmail"`
	Date          string     `json:"date"`
	Message       string     `json:"message"`
	Files         []FileStat `json:"files"`
	AIContributed bool       `json:"aiContributed"`
	AIAgents      []string   `json:"aiAgents,omitempty"`
}

func dataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "gitfolio")
	return dir, os.MkdirAll(dir, 0o700)
}

func newRepo(path string) Repo {
	sum := sha256.Sum256([]byte(path))
	return Repo{ID: hex.EncodeToString(sum[:6]), Path: path, Name: filepath.Base(path)}
}

func loadRepos(dir string) ([]Repo, error) {
	b, err := os.ReadFile(filepath.Join(dir, "repos.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var repos []Repo
	return repos, json.Unmarshal(b, &repos)
}

func saveRepos(dir string, repos []Repo) error {
	b, err := json.MarshalIndent(repos, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dir, "repos.json")
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func readCommits(dir string) ([]Commit, error) {
	f, err := os.Open(filepath.Join(dir, "commits.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var commits []Commit
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 16<<20) // long commit messages
	for sc.Scan() {
		var c Commit
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, sc.Err()
}

func appendCommits(dir string, commits []Commit) error {
	return encodeCommits(filepath.Join(dir, "commits.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, commits)
}

// writeCommits replaces commits.jsonl with commits.
func writeCommits(dir string, commits []Commit) error {
	p := filepath.Join(dir, "commits.jsonl")
	if err := encodeCommits(p+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, commits); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func encodeCommits(p string, flag int, commits []Commit) error {
	f, err := os.OpenFile(p, flag, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, c := range commits {
		if err := enc.Encode(c); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

// scanRepo stores the user's commits in r that are not stored yet and returns how many were added.
// With rebuild, r's stored commits are dropped first and collected again.
// ponytail: reads the full range and dedupes by hash on every scan; hooks pass push ranges in ROADMAP step 4.
// ponytail: no file lock; concurrent scans (hook + manual) can race until step 4 adds one.
func scanRepo(dir string, r *Repo, rebuild bool) (int, error) {
	mine, err := myEmails(r.Path)
	if err != nil {
		return 0, err
	}
	r.Provider, r.Namespace = remote(r.Path)
	rng, err := logRange(r.Path)
	if err != nil || rng == nil {
		return 0, err
	}
	out, err := gitLog(r.Path, rng)
	if err != nil {
		return 0, err
	}
	stored, err := readCommits(dir)
	if err != nil {
		return 0, err
	}
	if rebuild {
		kept := stored[:0]
		for _, c := range stored {
			if c.Repo != r.ID {
				kept = append(kept, c)
			}
		}
		if err := writeCommits(dir, kept); err != nil {
			return 0, err
		}
		stored = kept
	}
	known := map[string]bool{}
	for _, c := range stored {
		known[c.Hash] = true
	}
	var fresh []Commit
	for _, c := range parseLog(out) {
		if mine[strings.ToLower(c.AuthorEmail)] && !known[c.Hash] {
			c.Repo = r.ID
			fresh = append(fresh, c)
			known[c.Hash] = true
		}
	}
	if err := appendCommits(dir, fresh); err != nil {
		return 0, err
	}
	r.LastScan = time.Now().Format(time.RFC3339)
	return len(fresh), nil
}
