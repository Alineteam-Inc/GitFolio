package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestDetectLang(t *testing.T) {
	defer func(f func() []string) { osLanguages = f }(osLanguages)
	for _, tc := range []struct {
		env    map[string]string
		device []string // macOS preferred languages; nil elsewhere
		want   string
	}{
		{map[string]string{}, nil, "en"},
		{map[string]string{"LANG": "ko_KR.UTF-8"}, nil, "ko"},
		{map[string]string{"LANG": "ja_JP.UTF-8"}, nil, "ja"},
		{map[string]string{"LANG": "ko_KR.UTF-8", "LC_ALL": "C"}, nil, "en"}, // LC_ALL overrides LANG
		{map[string]string{"LANG": "en_US.UTF-8", "GITFOLIO_LANG": "ja"}, nil, "ja"},
		{map[string]string{"LANG": "fr_FR.UTF-8"}, nil, "en"},
		// The device language wins over a terminal that sets LANG=en_US whatever the device says.
		{map[string]string{"LANG": "en_US.UTF-8"}, []string{"ko-KR", "en-US"}, "ko"},
		{map[string]string{"LANG": "ko_KR.UTF-8"}, []string{"en-US", "ko-KR"}, "en"},
		{map[string]string{}, []string{"zh-Hans-KR", "ja-KR"}, "ja"},              // the first one GitFolio has
		{map[string]string{"LANG": "ko_KR.UTF-8"}, []string{"zh-Hans"}, "ko"},     // none of them: environment
		{map[string]string{"GITFOLIO_LANG": "ko"}, []string{"en-US", "ja"}, "ko"}, // GITFOLIO_LANG always wins
	} {
		osLanguages = func() []string { return tc.device }
		if got := detectLang(func(k string) string { return tc.env[k] }); got != tc.want {
			t.Errorf("detectLang(%v, device %v) = %q, want %q", tc.env, tc.device, got, tc.want)
		}
	}
}

func TestParseAppleLanguages(t *testing.T) {
	out := "(\n    \"ko-KR\",\n    en,\n    \"ja-KR\"\n)\n" // `defaults read -g AppleLanguages`
	if got := parseAppleLanguages(out); !slices.Equal(got, []string{"ko-KR", "en", "ja-KR"}) {
		t.Errorf("parseAppleLanguages = %q", got)
	}
}

// Every translation takes the same arguments as the English text, so no language prints %!s(MISSING).
func TestMessageVerbsMatch(t *testing.T) {
	verbs := regexp.MustCompile(`%(\[\d+\])?[-+# 0-9.]*[a-zA-Z]`)
	for key, byLang := range messages {
		want := verbs.FindAllString(byLang["en"], -1)
		slices.Sort(want)
		for _, lang := range []string{"ko", "ja"} {
			got := verbs.FindAllString(byLang[lang], -1)
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("message %q: %s has %v, en has %v", key, lang, got, want)
			}
		}
	}
	t.Setenv("GITFOLIO_LANG", "ko")
	if err := failure("notRegistered", "~/Code/demo"); err.Error() != "등록되지 않은 저장소입니다: ~/Code/demo" {
		t.Errorf("failure = %q", err)
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

// Status lines printed one after another form one paragraph with a single prefix; a notice, a question,
// a section or a blank line starts a new one.
func TestStatusParagraphs(t *testing.T) {
	var b strings.Builder
	inParagraph = false
	show(&b, "first")
	show(&b, "second\nthird")
	inParagraph = false // what notice, prompt, section and blank do
	show(&b, "fourth")
	under := strings.Repeat(" ", len(statusPrefix))
	want := margin + statusPrefix + "first\n" + margin + under + "second\n" + margin + under + "third\n" + margin + statusPrefix + "fourth\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
	inParagraph = false
}
