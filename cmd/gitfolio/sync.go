package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// syncState remembers what aline.team already has, so only new or changed records are sent (DESIGN 6.2).
type syncState struct {
	Account  string            `json:"account,omitempty"` // the aline.team account the records below were sent to
	Commits  map[string]string `json:"commits,omitempty"` // provider/namespace/hash → fingerprint of the record sent
	Deps     map[string]string `json:"deps,omitempty"`    // provider/namespace → fingerprint of the dependency list sent
	Deletes  []deletion        `json:"deletes,omitempty"` // deletions not yet accepted by the server
	LastSync string            `json:"lastSync,omitempty"`
}

// deletion is a queued delete request: one repository's data (remove --purge) or all dependencies (deps off).
type deletion struct {
	What      string `json:"what"` // "repository" or "dependencies"
	Provider  string `json:"provider,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type depItem struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
}

type repoDeps struct {
	Provider     string    `json:"provider"`
	Namespace    string    `json:"namespace"`
	Dependencies []depItem `json:"dependencies"`
}

// syncPayload is what the next sync sends, in order; `sync --dry-run` prints it as is.
type syncPayload struct {
	Deletes      []deletion `json:"deletes"`
	Commits      []Commit   `json:"commits"`
	Dependencies []repoDeps `json:"dependencies"`
	noRemote     int        // commits of repositories without a git service remote, never sent
}

// Limits of the data API (docs/API.md 4.1).
const (
	batchSize     = 500
	maxMessage    = 10000 // characters
	maxFilesSent  = 1000
	syncStateFile = "sync.json"
)

func loadSync(dir string) (st syncState, err error) {
	err = loadJSON(filepath.Join(dir, syncStateFile), &st)
	if st.Commits == nil {
		st.Commits = map[string]string{}
	}
	if st.Deps == nil {
		st.Deps = map[string]string{}
	}
	return st, err
}

func saveSync(dir string, st syncState) error { return saveJSON(filepath.Join(dir, syncStateFile), st) }

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func commitKey(c Commit) string { return c.Provider + "/" + c.Namespace + "/" + c.Hash }

// queueDeletion records a delete request for the next sync and forgets what was sent for it, so the
// data is sent again if it comes back. Callers hold the data lock.
func queueDeletion(dir string, d deletion) error {
	st, err := loadSync(dir)
	if err != nil {
		return err
	}
	if d.What == "repository" {
		if d.Namespace == "" {
			return nil // never sent: nothing to delete on the server
		}
		prefix := d.Provider + "/" + d.Namespace
		for k := range st.Commits {
			if strings.HasPrefix(k, prefix+"/") {
				delete(st.Commits, k)
			}
		}
		delete(st.Deps, prefix)
	} else {
		clear(st.Deps)
	}
	if !slices.Contains(st.Deletes, d) {
		st.Deletes = append(st.Deletes, d)
	}
	return saveSync(dir, st)
}

// pending works out what aline.team does not have yet. Records are the ones `gitfolio export` shows,
// cut to the server's limits.
func pending(dir string, st syncState) (p syncPayload, err error) {
	out, err := buildExport(dir)
	if err != nil {
		return p, err
	}
	p = syncPayload{Deletes: append([]deletion{}, st.Deletes...), Commits: []Commit{}, Dependencies: []repoDeps{}}
	for _, c := range out.Commits {
		if c.Namespace == "" {
			p.noRemote++
			continue
		}
		if utf8.RuneCountInString(c.Message) > maxMessage {
			c.Message = string([]rune(c.Message)[:maxMessage])
		}
		if len(c.Files) > maxFilesSent {
			c.Files = c.Files[:maxFilesSent]
		}
		if st.Commits[commitKey(c)] != fingerprint(c) {
			p.Commits = append(p.Commits, c)
		}
	}
	cfg, err := loadConfig(dir)
	if err != nil || !cfg.Deps {
		return p, err
	}
	repos, err := loadRepos(dir)
	if err != nil {
		return p, err
	}
	for _, r := range repos {
		if r.Namespace == "" {
			continue
		}
		list := []depItem{}
		for _, d := range out.Dependencies {
			if d.Provider == r.Provider && d.Namespace == r.Namespace {
				list = append(list, depItem{d.Ecosystem, d.Name, d.Version})
			}
		}
		key := r.Provider + "/" + r.Namespace
		if sent, ok := st.Deps[key]; (ok || len(list) > 0) && sent != fingerprint(list) {
			p.Dependencies = append(p.Dependencies, repoDeps{r.Provider, r.Namespace, list})
		}
	}
	return p, nil
}

type syncCounts struct{ commits, deps, deletes, noRemote int }

// syncData sends what aline.team does not have yet: queued deletions, new or changed commits, then
// dependency lists. Progress is saved as it goes, so after a failure only what was not accepted is sent
// again. Callers hold the data lock.
// ponytail: the lock stays held while sending; other commands wait up to 30s. Send outside the lock
// if large first syncs make that a problem.
func syncData(dir string, dryRun bool) (n syncCounts, err error) {
	c, err := newClient(dir)
	if err != nil {
		return n, err
	}
	st, err := loadSync(dir)
	if err != nil {
		return n, err
	}
	if st.Account != c.creds.Email {
		if st.Account != "" { // another account: it has none of this device's records yet
			st = syncState{Commits: map[string]string{}, Deps: map[string]string{}}
		}
		st.Account = c.creds.Email
	}
	p, err := pending(dir, st)
	if err != nil {
		return n, err
	}
	n.noRemote = p.noRemote
	if dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return n, enc.Encode(p)
	}
	save := func() error { return saveSync(dir, st) }

	for _, d := range p.Deletes {
		path := "/cli/dependencies"
		if d.What == "repository" {
			path = "/cli/repositories?" + url.Values{"provider": {d.Provider}, "namespace": {d.Namespace}}.Encode()
		}
		if err := c.call("DELETE", path, nil, nil); err != nil {
			return n, errors.Join(err, save())
		}
		st.Deletes = slices.DeleteFunc(st.Deletes, func(x deletion) bool { return x == d })
		n.deletes++
	}
	for batch := range slices.Chunk(p.Commits, batchSize) {
		err := c.upsert(batch, func(x Commit, accepted bool) {
			st.Commits[commitKey(x)] = fingerprint(x)
			if accepted {
				n.commits++
			}
		})
		if serr := save(); err != nil || serr != nil {
			return n, errors.Join(err, serr)
		}
	}
	for _, d := range p.Dependencies {
		if err := c.call("PUT", "/cli/dependencies", d, nil); err != nil {
			return n, errors.Join(err, save())
		}
		st.Deps[d.Provider+"/"+d.Namespace] = fingerprint(d.Dependencies)
		n.deps++
	}
	st.LastSync = time.Now().Format(time.RFC3339)
	return n, save()
}

// upsert sends commits and calls done for each one the server has dealt with. A batch the server rejects
// as malformed (C001) is split to find the bad records; those are skipped with a warning and marked done
// (not accepted), so they are not sent on every push, until they change.
func (c *client) upsert(commits []Commit, done func(c Commit, accepted bool)) error {
	err := c.call("POST", "/cli/commits/batch", map[string]any{"commits": commits}, nil)
	var ae *apiError
	if errors.As(err, &ae) && ae.Code == codeBadInput {
		if len(commits) == 1 {
			warn(detectLang(os.Getenv), "commitRejected", commits[0].Repo, commits[0].Hash[:min(12, len(commits[0].Hash))], ae)
			done(commits[0], false)
			return nil
		}
		h := len(commits) / 2
		if err := c.upsert(commits[:h], done); err != nil {
			return err
		}
		return c.upsert(commits[h:], done)
	}
	if err != nil {
		return err
	}
	for _, x := range commits {
		done(x, true)
	}
	return nil
}

// cmdSync collects new commits in every registered repository and sends what aline.team does not have.
func cmdSync(dir string, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print what would be sent, send nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lang := detectLang(os.Getenv)
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	for i := range repos {
		if _, err := scanRepo(dir, &repos[i], false); err != nil {
			warn(lang, "repoFailed", repos[i].Name, err)
		}
	}
	if err := saveRepos(dir, repos); err != nil {
		return err
	}
	n, err := syncData(dir, *dryRun)
	var ae *apiError
	if err != nil && (!errors.As(err, &ae) || ae.Status >= 500 || ae.Code == codeRateLimited) {
		return errors.New(strings.TrimSpace(fmt.Sprintf(tr(lang, "syncLater"), err))) // offline, server trouble or too many requests
	}
	if err != nil || *dryRun {
		return err
	}
	if n.commits+n.deps+n.deletes == 0 {
		say(lang, "upToDate")
	} else {
		say(lang, "synced", n.commits, n.deps, n.deletes)
	}
	if n.noRemote > 0 {
		say(lang, "noRemote", n.noRemote)
	}
	return nil
}
