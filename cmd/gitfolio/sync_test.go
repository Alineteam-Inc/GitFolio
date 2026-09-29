package main

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// TestSync walks the data API contract (docs/API.md 4) against the fake server: only new or changed
// records are sent, a record the server rejects is skipped, a failed send is retried, and queued
// deletions reach the server.
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
	sync := func(wantCommits, wantDeps, wantDeletes int) {
		t.Helper()
		n, err := syncData(dir, false)
		if err != nil || n.commits != wantCommits || n.deps != wantDeps || n.deletes != wantDeletes {
			t.Fatalf("sync = %+v, %v; want %d commits, %d dependency lists, %d deletions", n, err, wantCommits, wantDeps, wantDeletes)
		}
	}
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com"}))
	must(saveConfig(dir, Config{Deps: true}))
	must(saveRepos(dir, []Repo{
		{ID: "r1", Path: "/x/app", Name: "app", Provider: "GITHUB", Namespace: "me/app"},
		{ID: "r2", Path: "/x/local", Name: "local"}, // no remote: never sent
	}))
	commit := func(repo, hash, msg string) Commit {
		return Commit{Repo: repo, Hash: hash, AuthorEmail: "dev@example.com", Date: "2026-09-29T10:00:00+09:00", Message: msg,
			Files: []FileStat{{Name: "package.json", Add: 1, Module: "m1"}}, CreationType: "HUMAN"}
	}
	commits := []Commit{commit("r1", "a1", "feat: one"), commit("r1", "a2", "bad"), commit("r1", "a3", "fix: three"), commit("r2", "b1", "local")}
	must(writeCommits(dir, commits))
	must(saveJSON(filepath.Join(dir, "deps.json"), []Dependency{{Repo: "r1", Module: "m1", Ecosystem: "npm", Name: "react", Version: "18.2.0"}}))

	if _, err := syncData(dir, true); err != nil || len(f.commits) != 0 {
		t.Fatalf("dry run sent %d commits (%v)", len(f.commits), err)
	}
	sync(2, 1, 0) // a2 is rejected (C001) and skipped, b1 has no remote
	if _, ok := f.commits["GITHUB/me/app/a3"]; !ok || len(f.commits) != 2 || f.deps["GITHUB/me/app"][0].Name != "react" {
		t.Fatalf("server has %v, deps %v", f.commits, f.deps)
	}
	if got := f.commits["GITHUB/me/app/a1"]; got.Repo != "app" || got.Files[0].Module != "" {
		t.Errorf("sent record %+v: want the repository name and no module ID", got)
	}
	batches := f.batches
	sync(0, 0, 0) // nothing new
	if f.batches != batches {
		t.Error("unchanged records were sent again")
	}

	commits[0].Message = "feat: one [MASKED]" // e.g. after `config mask add`
	must(writeCommits(dir, commits))
	sync(1, 0, 0)

	f.down = true
	commits[2].Message = "fix: three again"
	must(writeCommits(dir, commits))
	if _, err := syncData(dir, false); err == nil {
		t.Fatal("sync to a server that is down succeeded")
	}
	f.down = false
	sync(1, 0, 0) // what failed is sent by the next sync

	// deps off, then remove --purge: both deletions reach the server on the next sync.
	must(queueDeletion(dir, deletion{What: "dependencies"}))
	must(saveConfig(dir, Config{Deps: false}))
	must(queueDeletion(dir, deletion{What: "repository", Provider: "GITHUB", Namespace: "me/app"}))
	must(writeCommits(dir, commits[3:]))
	must(saveRepos(dir, []Repo{{ID: "r2", Path: "/x/local", Name: "local"}}))
	sync(0, 0, 2)
	if len(f.commits) != 0 || len(f.deps) != 0 {
		t.Errorf("after the deletions the server still has %v, deps %v", f.commits, f.deps)
	}

	// Another account on this device gets everything again.
	must(writeCommits(dir, commits[:1]))
	must(saveRepos(dir, []Repo{{ID: "r1", Path: "/x/app", Name: "app", Provider: "GITHUB", Namespace: "me/app"}}))
	sync(1, 0, 0)
	must(saveCredentials(dir, Credentials{Token: testToken, Email: "other@example.com"}))
	sync(1, 0, 0)
}
