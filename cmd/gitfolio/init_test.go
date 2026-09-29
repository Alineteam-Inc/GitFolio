package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectLang(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{}, "en"},
		{map[string]string{"LANG": "ko_KR.UTF-8"}, "ko"},
		{map[string]string{"LANG": "ja_JP.UTF-8"}, "ja"},
		{map[string]string{"LANG": "ko_KR.UTF-8", "LC_ALL": "C"}, "en"}, // LC_ALL overrides LANG
		{map[string]string{"LANG": "en_US.UTF-8", "GITFOLIO_LANG": "ja"}, "ja"},
		{map[string]string{"LANG": "fr_FR.UTF-8"}, "en"},
	} {
		if got := detectLang(func(k string) string { return tc.env[k] }); got != tc.want {
			t.Errorf("detectLang(%v) = %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestMessagesComplete(t *testing.T) {
	for key, byLang := range messages {
		for _, lang := range []string{"en", "ko", "ja"} {
			if byLang[lang] == "" {
				t.Errorf("message %q has no %s text", key, lang)
			}
		}
	}
	for _, line := range strings.Split(header(), "\n") {
		if len(line) > 80 {
			t.Errorf("header line wider than 80 columns: %q", line)
		}
	}
}

func TestSuggestRoots(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"Code", "Documents/GitHub", "Music"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := suggestRoots(home) // "code" must not show up twice on case-insensitive disks
	want := []string{filepath.Join(home, "Code"), filepath.Join(home, "Documents", "GitHub")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("suggestRoots = %v, want %v", got, want)
	}
}

// TestFindRepos builds a folder tree with repositories where they should and should not be found.
func TestFindRepos(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	gitIn := func(dir string, args ...string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=me", "GIT_AUTHOR_EMAIL=me@example.com", "GIT_COMMITTER_NAME=me", "GIT_COMMITTER_EMAIL=me@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for _, d := range []string{"a", "group/b", "a/nested", ".hidden/c", "node_modules/d", "1/2/3/4/5/deep"} {
		gitIn(filepath.Join(root, d), "init", "-q", "-b", "main")
	}
	gitIn(filepath.Join(root, "a"), "config", "user.email", "me@example.com")
	gitIn(filepath.Join(root, "a"), "commit", "-q", "--allow-empty", "-m", "mine")
	gitIn(filepath.Join(root, "a"), "worktree", "add", "-q", filepath.Join(root, "a-wt"))

	var names []string
	for _, p := range findRepos([]string{root}, 5) {
		rel, _ := filepath.Rel(root, p)
		if r, err := filepath.EvalSymlinks(root); err == nil { // macOS temp dirs sit behind /var -> /private/var
			if rr, err := filepath.Rel(r, p); err == nil && !strings.HasPrefix(rr, "..") {
				rel = rr
			}
		}
		names = append(names, filepath.ToSlash(rel))
	}
	// a-wt is a worktree of a; a/nested is inside a; .hidden, node_modules and depth 6 are skipped.
	if got := strings.Join(names, " "); got != "a group/b" {
		t.Errorf("findRepos = %q, want %q", got, "a group/b")
	}
	if n, last := ownCommits(filepath.Join(root, "a")); n != 1 || len(last) != 10 {
		t.Errorf("ownCommits = %d, %q; want 1 and a YYYY-MM-DD date", n, last)
	}
}

// The token file must be readable by its owner only.
func TestCredentialsPermission(t *testing.T) {
	dir := t.TempDir()
	if err := saveCredentials(dir, Credentials{Token: testToken}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(dir, "credentials.json"))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("credentials.json mode = %v, want 0600", st.Mode().Perm())
		}
	}
}
