package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	must(writeCommits(dir, commits))

	if _, err := syncData(dir, true); err != nil || len(f.commits) != 0 {
		t.Fatalf("dry run sent %d commits (%v)", len(f.commits), err)
	}
	sync(2, 0) // a2 is rejected (C001) and skipped, b1 has no remote, c1's namespace is too long
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
