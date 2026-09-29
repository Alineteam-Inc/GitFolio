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
	"slices"
	"time"
)

type Repo struct {
	ID        string `json:"id"`
	Path      string `json:"path"` // local only, never exported or sent
	Name      string `json:"name"`
	Provider  string `json:"provider,omitempty"`  // GITHUB, GITLAB, ... or OTHER
	Namespace string `json:"namespace,omitempty"` // owner/repo on the git service
	LastScan  string `json:"last_scan,omitempty"`
	// Manifests records the user's decision per package manager file path: true = may be read.
	// Local only, never exported or sent. Files missing here wait for approval.
	Manifests map[string]bool `json:"manifests,omitempty"`
}

type FileStat struct {
	Name   string `json:"name"`
	Add    int    `json:"add"`
	Del    int    `json:"del"`
	Module string `json:"module,omitempty"` // local only: nearest approved manifest's directory, never exported
	path   string // full path, used for module lookup only, never stored
}

type Commit struct {
	Repo         string     `json:"repo,omitempty"`      // Repo.ID locally, repository name in export, left out when sent
	Provider     string     `json:"provider,omitempty"`  // filled in export only
	Namespace    string     `json:"namespace,omitempty"` // filled in export only
	Hash         string     `json:"hash"`
	Branch       string     `json:"branch,omitempty"` // remote branch it is on (see remoteBranches), never masked
	AuthorEmail  string     `json:"authorEmail"`
	Date         string     `json:"date"`
	Message      string     `json:"message"`
	Files        []FileStat `json:"files"`
	CreationType string     `json:"creationType"` // HUMAN, HUMAN_CO_AI or AI_CO_HUMAN
	AIAgents     []string   `json:"aiAgents,omitempty"`
	coAuthors    []string   // raw Co-authored-by emails, used for matching only, never stored
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

type Config struct {
	Mask      []string `json:"mask,omitempty"`      // blocked words: customer and internal project names
	Deps      bool     `json:"deps"`                // user allowed dependency detection (DESIGN 3.5)
	DepsAsked bool     `json:"depsAsked,omitempty"` // init asked once; later changes go through `gitfolio deps`
	Roots     []string `json:"roots,omitempty"`     // folders where the user keeps repositories (init)
	APIURL    string   `json:"apiUrl,omitempty"`    // aline.team API root; empty = production (config api-url)
	// AutoSyncOff stops sending right after git push; sync still sends (config autosync).
	AutoSyncOff bool `json:"autoSyncOff,omitempty"`
}

func loadRepos(dir string) (repos []Repo, err error) {
	err = loadJSON(filepath.Join(dir, "repos.json"), &repos)
	return repos, err
}

func saveRepos(dir string, repos []Repo) error {
	return saveJSON(filepath.Join(dir, "repos.json"), repos)
}

func loadConfig(dir string) (cfg Config, err error) {
	err = loadJSON(filepath.Join(dir, "config.json"), &cfg)
	return cfg, err
}

func saveConfig(dir string, cfg Config) error {
	return saveJSON(filepath.Join(dir, "config.json"), cfg)
}

// loadJSON decodes p into v; a missing file leaves v untouched.
func loadJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func saveJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
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

// agentTag is what the post-commit hook records when an AI agent's environment made the commit.
type agentTag struct {
	Hash   string   `json:"hash"`
	Agents []string `json:"agents"`
}

func appendAgentTag(dir string, t agentTag) error {
	f, err := os.OpenFile(filepath.Join(dir, "agent-tags.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(t); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readAgentTags returns the recorded agents by commit hash.
// ponytail: the file only grows; prune tags of stored commits if it ever gets large.
func readAgentTags(dir string) (map[string][]string, error) {
	f, err := os.Open(filepath.Join(dir, "agent-tags.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tags := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var t agentTag
		if json.Unmarshal(sc.Bytes(), &t) == nil { // skip a line cut short by a crash
			tags[t.Hash] = append(tags[t.Hash], t.Agents...)
		}
	}
	return tags, sc.Err()
}

// scanRepo stores the user's commits in r that are not stored yet and returns how many were added.
// With rebuild, r's stored commits are dropped first and collected again. Callers hold the data lock.
// ponytail: reads every pushed commit and dedupes by hash on each scan; pass push ranges if big repos get slow.
func scanRepo(dir string, r *Repo, rebuild bool) (int, error) {
	mine, err := myEmails(r.Path)
	if err != nil {
		return 0, err
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return 0, err
	}
	m := newMasker(cfg.Mask)
	r.Name = m.apply(filepath.Base(r.Path))
	r.Provider, r.Namespace = remote(r.Path)
	var modules map[string]string
	if cfg.Deps {
		if modules, err = refreshDeps(dir, r, m); err != nil {
			return 0, err
		}
	}
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
	// Commits stored before branches were recorded are collected once more, to get their branch.
	if rng[0] == "--remotes" && slices.ContainsFunc(stored, func(c Commit) bool { return c.Repo == r.ID && c.Branch == "" }) {
		rebuild = true
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
	tags, err := readAgentTags(dir)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, c := range stored {
		known[c.Hash] = true
	}
	var fresh []Commit
	for _, c := range parseLog(out) {
		if known[c.Hash] {
			continue
		}
		if extra := tags[c.Hash]; len(extra) > 0 {
			c.AIAgents = slices.Compact(slices.Sorted(slices.Values(append(c.AIAgents, extra...))))
		}
		if c.CreationType = creationType(c, mine); c.CreationType != "" {
			c.Repo = r.ID
			for i := range c.Files {
				c.Files[i].Module = moduleOf(c.Files[i].path, modules)
			}
			m.commit(&c) // after AI detection in parseLog, before anything is written
			fresh = append(fresh, c)
			known[c.Hash] = true
		}
	}
	if len(fresh) > 0 && rng[0] == "--remotes" {
		branches, err := remoteBranches(r.Path)
		if err != nil {
			return 0, err
		}
		for i := range fresh {
			fresh[i].Branch = branches[fresh[i].Hash] // as is, so aline.team matches it with its GitHub sync
		}
	}
	if err := appendCommits(dir, fresh); err != nil {
		return 0, err
	}
	r.LastScan = time.Now().Format(time.RFC3339)
	return len(fresh), nil
}
