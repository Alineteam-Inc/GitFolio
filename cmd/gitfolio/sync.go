package main

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
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
	Account   string            `json:"account,omitempty"`   // the aline.team account the records below were sent to
	Commits   map[string]string `json:"commits,omitempty"`   // provider/namespace/hash → fingerprint of the record sent
	Deletes   []deletion        `json:"deletes,omitempty"`   // repository deletions not yet accepted by the server
	Changed   map[string]string `json:"changed,omitempty"`   // provider/namespace/path → modifiedAt sent (modifiedFiles)
	LastSync  string            `json:"lastSync,omitempty"`  // last attempt, shown by `gitfolio schedule`
	LastError string            `json:"lastError,omitempty"` // why it failed; empty when it went through
}

// deletion asks aline.team to delete one repository's commits (remove --purge).
type deletion struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
}

// syncPayload is what the next sync sends, in order. Dependencies are not sent yet: aline.team designs
// that API in its second phase (docs/API.md 4).
type syncPayload struct {
	Deletes  []deletion                `json:"deletes"`
	Commits  []Commit                  `json:"commits"`
	modified map[string][]modifiedFile // provider/namespace → first changes by others not sent yet
	noRemote int                       // commits of repositories without a git service remote, never sent
}

// modifiedFile tells aline.team when someone else first changed a file the user created.
type modifiedFile struct {
	Name       string `json:"name"`
	ModifiedAt string `json:"modifiedAt"`
}

// commitBatch is one POST /cli/commits/batch request: one repository and its commits (docs/API.md 4.1).
// The server knows the repository by provider and namespace; the local name is not sent.
type commitBatch struct {
	Provider      string         `json:"provider"`
	Namespace     string         `json:"namespace"`
	AuthorEmail   string         `json:"authorEmail"` // the primary work email, for all of the user's commits
	Commits       []Commit       `json:"commits"`
	ModifiedFiles []modifiedFile `json:"modifiedFiles,omitempty"` // first changes by others, at most maxModified
}

// request builds the body for commits of one repository: the repository fields and the author email
// move to the top, and every commit goes under author, the primary work email.
func request(commits []Commit, author string, modified []modifiedFile) commitBatch {
	b := commitBatch{Provider: commits[0].Provider, Namespace: commits[0].Namespace, AuthorEmail: cmp.Or(author, commits[0].AuthorEmail), ModifiedFiles: modified}
	for _, c := range commits {
		c.Provider, c.Namespace, c.Repo, c.AuthorEmail = "", "", "", ""
		b.Commits = append(b.Commits, c)
	}
	return b
}

// Limits of the data API (docs/API.md 4.1).
const (
	batchSize     = 500   // records per request
	maxMessage    = 10000 // characters
	maxFilesSent  = 1000
	maxModified   = 5000 // modifiedFiles per request
	maxNamespace  = 200  // the server keys repositories by "[internal]"
	syncStateFile = "sync.json"
)

func loadSync(dir string) (st syncState, err error) {
	err = loadJSON(filepath.Join(dir, syncStateFile), &st)
	if st.Commits == nil {
		st.Commits = map[string]string{}
	}
	if st.Changed == nil {
		st.Changed = map[string]string{}
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

// queueDeletion records a repository deletion for the next sync and forgets what was sent for it, so
// the commits are sent again if the repository is added back. Callers hold the data lock.
func queueDeletion(dir string, d deletion) error {
	if d.Namespace == "" {
		return nil // never sent: nothing to delete on the server
	}
	st, err := loadSync(dir)
	if err != nil {
		return err
	}
	for _, m := range []map[string]string{st.Commits, st.Changed} {
		for k := range m {
			if strings.HasPrefix(k, d.Provider+"/"+d.Namespace+"/") {
				delete(m, k)
			}
		}
	}
	if !slices.Contains(st.Deletes, d) {
		st.Deletes = append(st.Deletes, d)
	}
	return saveSync(dir, st)
}

// pending works out what aline.team does not have yet. Records are the ones `gitfolio export` shows,
// cut to the server's limits.
func pending(dir string, st syncState) (syncPayload, error) {
	p := syncPayload{Deletes: append([]deletion{}, st.Deletes...), Commits: []Commit{}, modified: map[string][]modifiedFile{}}
	out, err := buildExport(dir)
	if err != nil {
		return p, err
	}
	for _, c := range out.Commits {
		if c.Namespace == "" || len(c.Namespace) > maxNamespace {
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
	repos, err := loadRepos(dir)
	if err != nil {
		return p, err
	}
	for _, r := range repos {
		key := r.Provider + "/" + r.Namespace
		for _, name := range slices.Sorted(maps.Keys(r.Changed)) {
			if at := r.Changed[name]; r.Namespace != "" && st.Changed[key+"/"+name] != at {
				p.modified[key] = append(p.modified[key], modifiedFile{name, at})
			}
		}
	}
	return p, nil
}

// takeModified hands out the first changes of repository key for one request, at most maxModified.
// aline.team takes them only in a request with commits, so a repository without new commits keeps them
// for a later sync.
func (p *syncPayload) takeModified(key string) []modifiedFile {
	m := p.modified[key]
	n := min(len(m), maxModified)
	p.modified[key] = m[n:]
	return m[:n]
}

// batches splits commits into requests of one repository each, at most batchSize records.
func batches(commits []Commit) [][]Commit {
	commits = slices.Clone(commits)
	slices.SortStableFunc(commits, func(a, b Commit) int {
		return strings.Compare(a.Provider+"/"+a.Namespace, b.Provider+"/"+b.Namespace)
	})
	var out [][]Commit
	var cur []Commit
	for _, c := range commits {
		if len(cur) > 0 && (cur[0].Provider != c.Provider || cur[0].Namespace != c.Namespace || len(cur) == batchSize) {
			out, cur = append(out, cur), nil
		}
		cur = append(cur, c)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

type syncCounts struct{ commits, deletes, noRemote int }

// syncData sends what aline.team does not have yet and records when it tried and whether it worked.
// Callers hold the data lock.
func syncData(dir string, dryRun bool) (syncCounts, error) {
	n, err := sendPending(dir, dryRun)
	if dryRun {
		return n, err
	}
	st, lerr := loadSync(dir)
	if lerr != nil {
		return n, errors.Join(err, lerr)
	}
	st.LastSync, st.LastError = time.Now().Format(time.RFC3339), ""
	if err != nil {
		st.LastError = err.Error()
	}
	return n, errors.Join(err, saveSync(dir, st))
}

// sendPending sends queued deletions, then new or changed commits. Progress is saved as it goes, so
// after a failure only what was not accepted is sent again.
// ponytail: the lock stays held while sending; other commands wait up to 30s. Send outside the lock
// if large first syncs make that a problem.
func sendPending(dir string, dryRun bool) (n syncCounts, err error) {
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
			st = syncState{Commits: map[string]string{}, Changed: map[string]string{}}
		}
		st.Account = c.creds.Email
	}
	p, err := pending(dir, st)
	if err != nil {
		return n, err
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return n, err
	}
	author := primaryEmail(cfg)
	n.noRemote = p.noRemote
	if dryRun { // the requests exactly as they would be sent
		out := struct {
			Deletes       []deletion    `json:"deletes"`
			CommitBatches []commitBatch `json:"commitBatches"`
		}{p.Deletes, []commitBatch{}}
		for _, b := range batches(p.Commits) {
			out.CommitBatches = append(out.CommitBatches, request(b, author, p.takeModified(b[0].Provider+"/"+b[0].Namespace)))
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return n, enc.Encode(out)
	}
	save := func() error { return saveSync(dir, st) }

	for _, d := range p.Deletes {
		path := "/cli/repositories?" + url.Values{"provider": {d.Provider}, "namespace": {d.Namespace}}.Encode()
		if err := c.call("DELETE", path, nil, nil); err != nil {
			return n, errors.Join(err, save())
		}
		st.Deletes = slices.DeleteFunc(st.Deletes, func(x deletion) bool { return x == d })
		n.deletes++
	}
	for _, batch := range batches(p.Commits) {
		key := batch[0].Provider + "/" + batch[0].Namespace
		err := c.upsert(author, batch, p.takeModified(key), func(x Commit, accepted bool) {
			st.Commits[commitKey(x)] = fingerprint(x)
			if accepted {
				n.commits++
			}
		}, func(sent []modifiedFile) {
			for _, f := range sent {
				st.Changed[key+"/"+f.Name] = f.ModifiedAt
			}
		})
		if serr := save(); err != nil || serr != nil {
			return n, errors.Join(err, serr)
		}
	}
	return n, save()
}

// upsert sends commits and calls done for each one the server has dealt with. A batch the server rejects
// as malformed (C001) is split to find the bad records; those are skipped with a warning and marked done
// (not accepted), so they are not sent on every push, until they change.
// The first changes in modified go with the first part of a split batch; if that part is rejected they
// are not marked sent and go with a later sync.
func (c *client) upsert(author string, commits []Commit, modified []modifiedFile, done func(c Commit, accepted bool), sent func([]modifiedFile)) error {
	err := c.call("POST", "/cli/commits/batch", request(commits, author, modified), nil)
	var ae *apiError
	if errors.As(err, &ae) && ae.Code == codeBadInput {
		if len(commits) == 1 {
			warn(detectLang(os.Getenv), "commitRejected", commits[0].Repo, commits[0].Hash[:min(12, len(commits[0].Hash))], ae)
			done(commits[0], false)
			return nil
		}
		h := len(commits) / 2
		if err := c.upsert(author, commits[:h], modified, done, sent); err != nil {
			return err
		}
		return c.upsert(author, commits[h:], nil, done, sent)
	}
	if err != nil {
		return err
	}
	for _, x := range commits {
		done(x, true)
	}
	sent(modified)
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
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	depsToo := cfg.Deps && cfg.ScheduleDeps && os.Getenv("GITFOLIO_SCHEDULED") == "1" // schedule deps on
	for i := range repos {
		if _, err := scanRepo(dir, &repos[i], false); err != nil {
			warn(lang, "repoFailed", repos[i].Name, err)
			continue
		}
		if depsToo {
			if _, _, err := refreshDeps(dir, &repos[i]); err != nil {
				warn(lang, "repoFailed", repos[i].Name, err)
			}
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
	if n.commits+n.deletes == 0 {
		say(lang, "upToDate")
	} else {
		say(lang, "synced", n.commits, n.deletes)
	}
	if n.noRemote > 0 {
		say(lang, "noRemote", n.noRemote)
	}
	return nil
}
