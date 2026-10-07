package main

import (
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestSync walks the data API contract against the fake server: only new or changed
// records are sent, a record the server rejects is skipped, a failed send is retried, and a queued
// repository deletion reaches the server.
func TestSync(t *testing.T) {
	f := &fakeAline{token: testToken}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	t.Setenv("GITFOLIO_LANG", "en")
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	sync := func(wantCommits, wantDeletes int) {
		t.Helper()
		n, err := syncData(dir, false)
		if err != nil || n.commits != wantCommits || n.deletes != wantDeletes {
			t.Fatalf("sync = %+v, %v; want %d commits, %d deletions", n, err, wantCommits, wantDeletes)
		}
	}
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com"}))
	must(saveConfig(dir, Config{Emails: []string{"me@primary.com", "dev@example.com"}}))
	must(saveRepos(dir, []Repo{
		{ID: "r1", Path: "/x/app", Name: "app", Provider: "GITHUB", Namespace: "me/app", Changed: map[string]string{"main.go": "2026-09-30T11:00:00+09:00"}},
		{ID: "r2", Path: "/x/local", Name: "local"},                                                               // no remote: never sent
		{ID: "r3", Path: "/x/deep", Name: "deep", Provider: "GITLAB", Namespace: strings.Repeat("g/", 100) + "x"}, // over 200 characters
	}))
	commit := func(repo, hash, msg string) Commit {
		return Commit{Repo: repo, Hash: hash, Branch: "main", AuthorEmail: "dev@example.com", Date: "2026-09-29T10:00:00+09:00",
			Message: msg, Files: []FileStat{{Name: "main.go", Add: 1, Module: "m1"}}, CreationType: "HUMAN"}
	}
	commits := []Commit{commit("r1", "a1", "feat: one"), commit("r1", "a2", "bad"), commit("r1", "a3", "fix: three"), commit("r2", "b1", "local"), commit("r3", "c1", "deep")}
	commits[2].Files = make([]FileStat, maxFilesSent+5) // cut to the server's limit before sending
	for i := range commits[2].Files {
		commits[2].Files[i] = FileStat{Name: "f.go", Add: 1}
	}
	unpushed := commit("r1", "a4", "wip") // read from HEAD: the repository has a remote but no push succeeded yet
	unpushed.Branch = ""
	must(writeCommits(dir, append(commits, unpushed)))
	p, err := pending(dir, syncState{}) // checked here: aline.team rejects a missing branch (C001), so the sync below passes either way
	must(err)
	for _, c := range p.Commits {
		if c.Hash == "a4" {
			t.Error("a commit that was never pushed is pending")
		}
	}

	if _, err := syncData(dir, true); err != nil || len(f.commits) != 0 {
		t.Fatalf("dry run sent %d commits (%v)", len(f.commits), err)
	}
	sync(2, 0) // a2 is rejected (C001) and skipped, b1 has no remote, c1's namespace is too long, a4 is not pushed
	if _, ok := f.commits["GITHUB/me/app/a3"]; !ok || len(f.commits) != 2 {
		t.Fatalf("server has %v", f.commits)
	}
	if got := f.commits["GITHUB/me/app/a1"]; got.Repo != "" || got.Branch != "main" || got.Files[0].Module != "" || got.AuthorEmail != "me@primary.com" {
		t.Errorf("sent record %+v: want the branch, the primary email as author, and no repository name or module ID", got)
	}
	if f.modified["GITHUB/me/app/main.go"] != "2026-09-30T11:00:00+09:00" {
		t.Errorf("modifiedFiles = %v, want main.go with its first change by someone else", f.modified)
	}
	batches := f.batches
	sync(0, 0) // nothing new
	if f.batches != batches {
		t.Error("unchanged records were sent again")
	}
	// A new first change waits for a request with commits: aline.team takes none without.
	repos, _ := loadRepos(dir)
	repos[0].Changed["util.go"] = "2026-09-30T12:00:00+09:00"
	must(saveRepos(dir, repos))
	sync(0, 0)
	if _, ok := f.modified["GITHUB/me/app/util.go"]; ok || f.batches != batches {
		t.Error("modifiedFiles went without commits")
	}

	commits[0].Message = "feat: one [MASKED]" // e.g. after `config mask add`
	must(writeCommits(dir, commits))
	delete(f.modified, "GITHUB/me/app/main.go")
	sync(1, 0)
	if f.modified["GITHUB/me/app/util.go"] == "" || f.modified["GITHUB/me/app/main.go"] != "" {
		t.Errorf("with the next commits only the new first change goes: %v", f.modified)
	}

	f.down = true
	commits[2].Message = "fix: three again"
	must(writeCommits(dir, commits))
	if _, err := syncData(dir, false); err == nil {
		t.Fatal("sync to a server that is down succeeded")
	}
	if st, _ := loadSync(dir); st.LastError == "" || st.LastSync == "" {
		t.Errorf("failed sync not recorded: %+v", st)
	}
	f.down = false
	sync(1, 0) // what failed is sent by the next sync
	if st, _ := loadSync(dir); st.LastError != "" {
		t.Errorf("successful sync kept the old error %q", st.LastError)
	}

	// remove --purge: the deletion reaches the server on the next sync.
	must(queueDeletion(dir, deletion{"GITHUB", "me/app"}))
	must(writeCommits(dir, commits[3:]))
	must(saveRepos(dir, []Repo{{ID: "r2", Path: "/x/local", Name: "local"}}))
	sync(0, 1)
	if len(f.commits) != 0 {
		t.Errorf("after the deletion the server still has %v", f.commits)
	}

	// Another account on this device gets everything again.
	must(writeCommits(dir, commits[:1]))
	must(saveRepos(dir, []Repo{{ID: "r1", Path: "/x/app", Name: "app", Provider: "GITHUB", Namespace: "me/app"}}))
	sync(1, 0)
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "other@example.com"}))
	sync(1, 0)
}

// Requests hold one repository each and at most 500 records.
func TestBatches(t *testing.T) {
	var commits []Commit
	for i := range 1300 { // two repositories, interleaved
		commits = append(commits, Commit{Hash: "h", Provider: "GITHUB", Namespace: []string{"me/a", "me/b"}[i%2]})
	}
	total := 0
	for _, b := range batches(commits) {
		for _, c := range b {
			if c.Namespace != b[0].Namespace {
				t.Fatalf("a batch mixes %s and %s", b[0].Namespace, c.Namespace)
			}
		}
		if len(b) > batchSize {
			t.Errorf("batch of %d records", len(b))
		}
		total += len(b)
	}
	if total != len(commits) {
		t.Errorf("batches hold %d records, want %d", total, len(commits))
	}
}

// TestRemoteBranches checks the branch each commit is reported on: the default branch wins over other
// branches that also contain the commit, branch names are sent as they are (even with a blocked word),
// and commits stored before branches were recorded get theirs on the next scan.
func TestRemoteBranches(t *testing.T) {
	remoteDir, repo, dir := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commit := func(msg string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, msg+".txt"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-q", "-m", msg)
	}
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", remoteDir).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "me@example.com")
	git("config", "user.name", "me")
	git("remote", "add", "origin", "https://github.com/me/app.git")
	git("remote", "set-url", "--push", "origin", remoteDir)
	commit("on-main")
	git("push", "-q", "origin", "main")
	git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	git("switch", "-q", "-c", "aaa") // sorts before main, and also contains on-main
	commit("on-aaa")
	git("push", "-q", "origin", "aaa")
	git("switch", "-q", "-c", "feature/acme-login", "main")
	commit("on-feature")
	git("push", "-q", "origin", "feature/acme-login")

	if err := saveConfig(dir, Config{Mask: []string{"acme"}}); err != nil {
		t.Fatal(err)
	}
	r := newRepo(repo)
	check := func() {
		t.Helper()
		if _, err := scanRepo(dir, &r, false); err != nil {
			t.Fatal(err)
		}
		commits, err := readCommits(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, c := range commits {
			got[c.Message] = c.Branch
		}
		want := map[string]string{"on-main": "main", "on-aaa": "aaa", "on-feature": "feature/acme-login"}
		if len(got) != len(want) {
			t.Fatalf("branches = %v, want %v", got, want)
		}
		for msg, b := range want {
			if got[msg] != b {
				t.Errorf("%s is on %q, want %q (all: %v)", msg, got[msg], b, got)
			}
		}
	}
	check()

	commits, _ := readCommits(dir)
	for i := range commits {
		commits[i].Branch = "" // as stored by an older version
	}
	if err := writeCommits(dir, commits); err != nil {
		t.Fatal(err)
	}
	check()
}

// A repository aline.team takes only after an email is verified (A013) waits, and only it: the other
// repositories are sent, its commits are not marked sent, and they go once the email is verified.
func TestSyncWaitsForVerifiedEmail(t *testing.T) {
	f := &fakeAline{token: testToken, waitFor: map[string]string{"me/app": "me@work.com"}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com"}))
	must(saveConfig(dir, Config{Emails: []string{"me@work.com"}}))
	must(saveRepos(dir, []Repo{
		{ID: "r1", Path: "/x/app", Name: "app", Provider: "GITHUB", Namespace: "me/app"}, // waits
		{ID: "r2", Path: "/x/lib", Name: "lib", Provider: "GITHUB", Namespace: "me/lib"},
	}))
	commit := func(repo, hash string) Commit {
		return Commit{Repo: repo, Hash: hash, Branch: "main", Date: "2026-10-02T10:00:00+09:00", Message: "feat", CreationType: "HUMAN"}
	}
	must(writeCommits(dir, []Commit{commit("r1", "a1"), commit("r1", "a2"), commit("r2", "b1")}))

	n, err := syncData(dir, false)
	if err != nil || n.commits != 1 || len(f.commits) != 1 {
		t.Fatalf("sync = %+v, %v; server has %v: want only me/lib's commit", n, err, f.commits)
	}
	st, _ := loadSync(dir)
	if st.Waiting["GITHUB/me/app"] != "me@work.com" || st.LastError != "" {
		t.Errorf("waiting = %v, lastError %q", st.Waiting, st.LastError)
	}
	f.work = append(f.work, "me@work.com") // verified with `gitfolio email verify`
	if n, err := syncData(dir, false); err != nil || n.commits != 2 || len(f.commits) != 3 {
		t.Fatalf("after verifying: sync = %+v, %v; server has %d commits", n, err, len(f.commits))
	}
	if st, _ := loadSync(dir); len(st.Waiting) != 0 {
		t.Errorf("still waiting: %v", st.Waiting)
	}
}

// A repository whose git service address changed is renamed on aline.team before any commits go, and
// what was sent moves to the new name: nothing is sent again. An older server without the call gets
// the commits again under the new name; while it cannot be reached the move waits.
func TestSyncMovesRepository(t *testing.T) {
	f := &fakeAline{token: testToken}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com"}))
	must(saveConfig(dir, Config{Emails: []string{"dev@example.com"}}))
	at := func(provider, namespace string) {
		t.Helper()
		must(saveRepos(dir, []Repo{{ID: "r1", Path: "/x/app", Name: "app", Provider: provider, Namespace: namespace,
			Changed: map[string]string{"main.go": "2026-10-02T11:00:00+09:00"}}}))
	}
	long := strings.Repeat("x", maxMessage+10) // cut before it is sent and fingerprinted
	must(writeCommits(dir, []Commit{
		{Repo: "r1", Hash: "a1", Branch: "main", Date: "2026-10-02T10:00:00+09:00", Message: "feat", Files: []FileStat{{Name: "main.go", Created: true}}, CreationType: "HUMAN"},
		{Repo: "r1", Hash: "a2", Branch: "main", Date: "2026-10-02T10:00:00+09:00", Message: long, CreationType: "HUMAN"},
	}))
	at("GITLAB", "me/app")
	if n, err := syncData(dir, false); err != nil || n.commits != 2 {
		t.Fatalf("first sync = %+v, %v", n, err)
	}
	batches := f.batches

	// git remote set-url: now on GitHub.
	must(queueMove(dir, repoMove{deletion{"GITLAB", "me/app"}, deletion{"GITHUB", "me/app"}}))
	at("GITHUB", "me/app")
	if out := dryRun(t, dir); !strings.Contains(out, `"moves"`) || strings.Contains(out, `"hash"`) {
		t.Errorf("dry run before the move shows commits to send again:\n%s", out)
	}
	n, err := syncData(dir, false)
	if err != nil || n.moves != 1 || n.commits != 0 || f.batches != batches {
		t.Fatalf("after the move: sync = %+v, %v, batches %d → %d: want the move and nothing sent again", n, err, batches, f.batches)
	}
	if _, ok := f.commits["GITHUB/me/app/a2"]; !ok || len(f.commits) != 2 {
		t.Errorf("server has %v", f.commits)
	}
	if st, _ := loadSync(dir); len(st.Moves) != 0 || st.Changed["GITHUB/me/app/main.go"] == "" || st.Changed["GITLAB/me/app/main.go"] != "" {
		t.Errorf("sync state after the move: moves %v, changed %v", st.Moves, st.Changed)
	}

	// Chained moves are one; one back to where it started is none.
	must(queueMove(dir, repoMove{deletion{"GITHUB", "me/app"}, deletion{"GITHUB", "me/app2"}}))
	must(queueMove(dir, repoMove{deletion{"GITHUB", "me/app2"}, deletion{"GITHUB", "me/app3"}}))
	if st, _ := loadSync(dir); len(st.Moves) != 1 || st.Moves[0].From.Namespace != "me/app" || st.Moves[0].To.Namespace != "me/app3" {
		t.Errorf("chained: %v", st.Moves)
	}
	must(queueMove(dir, repoMove{deletion{"GITHUB", "me/app3"}, deletion{"GITHUB", "me/app"}}))
	if st, _ := loadSync(dir); len(st.Moves) != 0 {
		t.Errorf("moved back: %v", st.Moves)
	}

	// Offline: the move waits, and no commits go under the new name before it.
	must(queueMove(dir, repoMove{deletion{"GITHUB", "me/app"}, deletion{"GITHUB", "me/new"}}))
	at("GITHUB", "me/new")
	f.down = true
	if _, err := syncData(dir, false); err == nil {
		t.Fatal("sync to a server that is down succeeded")
	}
	if st, _ := loadSync(dir); len(st.Moves) != 1 {
		t.Errorf("the move was not kept: %v", st.Moves)
	}
	// An older server: the move is dropped and the commits go again under the new name.
	f.down, f.noMove = false, true
	if n, err := syncData(dir, false); err != nil || n.moves != 0 || n.commits != 2 {
		t.Fatalf("older server: sync = %+v, %v", n, err)
	}
	if st, _ := loadSync(dir); len(st.Moves) != 0 {
		t.Errorf("the move stayed: %v", st.Moves)
	}
}

// A move aline.team has nothing for (another device moved it first) is not counted, and still moves
// what this device sent.
func TestSyncMoveNoop(t *testing.T) {
	f := &fakeAline{token: testToken}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	dir := t.TempDir()
	if err := saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := queueMove(dir, repoMove{deletion{"GITHUB", "me/a"}, deletion{"GITHUB", "me/b"}}); err != nil {
		t.Fatal(err)
	}
	if n, err := syncData(dir, false); err != nil || n.moves != 0 || f.moves != 1 {
		t.Errorf("sync = %+v, %v; server moves %d", n, err, f.moves)
	}
	if st, _ := loadSync(dir); len(st.Moves) != 0 {
		t.Errorf("moves left: %v", st.Moves)
	}
}

// dryRun returns what `gitfolio sync --dry-run` prints.
func dryRun(t *testing.T, dir string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	_, err = syncData(dir, true)
	os.Stdout = stdout
	w.Close()
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestServerSwitch: a login belongs to the server that issued it. A build that talks to another
// server (a release build always uses production) counts as logged out and never sends that token,
// and after logging in there everything is sent again, since that server has none of it yet.
func TestServerSwitch(t *testing.T) {
	f := &fakeAline{token: testToken}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	t.Setenv("GITFOLIO_LANG", "en")
	home := t.TempDir() // run below finds the data folder from these, never the real one
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	dir, err := dataDir()
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const dev = "https://dev.example/api"
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: dev}))
	if c, err := newClient(dir); err != nil || c.loggedIn() {
		t.Fatalf("a token from %s counts here (%v)", dev, err)
	}
	if err := run([]string{"scan"}); err == nil || !strings.Contains(err.Error(), "gitfolio login") {
		t.Errorf("scan with another server's login: %v, want a login hint", err)
	}

	must(saveConfig(dir, Config{Emails: []string{"dev@example.com"}}))
	must(saveRepos(dir, []Repo{{ID: "r1", Path: "/x/app", Name: "app", Provider: "GITHUB", Namespace: "me/app"}}))
	must(writeCommits(dir, []Commit{{Repo: "r1", Hash: "a1", Branch: "main", AuthorEmail: "dev@example.com",
		Date: "2026-09-29T10:00:00+09:00", Message: "feat: one", CreationType: "HUMAN"}}))
	must(saveSync(dir, syncState{Account: "dev@example.com", Server: dev, Commits: map[string]string{"GITHUB/me/app/a1": "sent to dev"}}))
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com", Server: srv.URL})) // logged in here
	if n, err := syncData(dir, false); err != nil || n.commits != 1 {
		t.Fatalf("sync after switching servers = %+v, %v; want the commit sent again", n, err)
	}
}

// Dependencies go after the commits, per repository and only when they changed: names only, of the
// modules the user's commits touched, for repositories aline.team has; deps off empties them there.
func TestSyncDependencies(t *testing.T) {
	f := &fakeAline{token: testToken, waitFor: map[string]string{"me/wait": "work@example.com"}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	t.Setenv("GITFOLIO_LANG", "en")
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com"}))
	must(saveConfig(dir, Config{Emails: []string{"dev@example.com"}, Deps: true}))
	must(saveRepos(dir, []Repo{
		{ID: "r1", Name: "app", Provider: "GITHUB", Namespace: "me/app"},
		{ID: "r2", Name: "wait", Provider: "GITHUB", Namespace: "me/wait"}, // waits for a verified email (A013)
		{ID: "r3", Name: "new", Provider: "GITHUB", Namespace: "me/new"},   // never pushed: aline.team has no such repository
	}))
	commit := func(repo, hash, branch string) Commit {
		return Commit{Repo: repo, Hash: hash, Branch: branch, AuthorEmail: "dev@example.com", Date: "2026-10-07T10:00:00+09:00",
			Message: "feat", Files: []FileStat{{Name: "main.go", Add: 1, Module: "m1"}}, CreationType: "HUMAN"}
	}
	must(writeCommits(dir, []Commit{commit("r1", "a1", "main"), commit("r2", "b1", "main"), commit("r3", "c1", "")}))
	deps := []Dependency{
		{Repo: "r1", Module: "m1", Ecosystem: "npm", Name: "react", Version: "18.2.0"},
		{Repo: "r1", Module: "m1", Ecosystem: "npm", Name: "next"},
		{Repo: "r1", Module: "m1", Ecosystem: "npm", Name: strings.Repeat("x", 215)}, // longer than aline.team takes
		{Repo: "r1", Module: "m2", Ecosystem: "npm", Name: "vue"},                    // a module the user never touched
		{Repo: "r2", Module: "m1", Ecosystem: "npm", Name: "express"},
		{Repo: "r3", Module: "m1", Ecosystem: "go", Name: "github.com/gin-gonic/gin"},
	}
	must(saveJSON(filepath.Join(dir, "deps.json"), deps))

	n, err := syncData(dir, false)
	if err != nil || n.commits != 1 || n.deps != 1 {
		t.Fatalf("sync = %+v, %v; want 1 commit and 1 repository's dependencies", n, err)
	}
	if got := f.deps["GITHUB/me/app"]; !slices.Equal(got, []string{"npm:next", "npm:react"}) || len(f.deps) != 1 {
		t.Fatalf("aline.team has dependencies %v", f.deps)
	}
	calls := f.depsCalls
	must2 := func() syncCounts {
		t.Helper()
		n, err := syncData(dir, false)
		must(err)
		return n
	}
	if must2(); f.depsCalls != calls {
		t.Error("unchanged dependencies were sent again")
	}
	deps = append(deps, Dependency{Repo: "r1", Module: "m1", Ecosystem: "npm", Name: "vite"})
	must(saveJSON(filepath.Join(dir, "deps.json"), deps))
	if n := must2(); n.deps != 1 || len(f.deps["GITHUB/me/app"]) != 3 {
		t.Errorf("a new dependency: %+v, aline.team has %v", n, f.deps)
	}
	must(saveJSON(filepath.Join(dir, "deps.json"), []Dependency{})) // deps off
	if n := must2(); n.deps != 1 || len(f.deps["GITHUB/me/app"]) != 0 {
		t.Errorf("deps off: %+v, aline.team still has %v", n, f.deps)
	}
	calls = f.depsCalls
	if must2(); f.depsCalls != calls {
		t.Error("the empty list was sent again")
	}
}
