package main

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
)

// logo is ASCII only so it renders in any terminal font, including Windows consoles (DESIGN 7.2).
const logo = `    _     _      ___  _   _  _____  _____  _____     _     __  __
   / \   | |    |_ _|| \ | || ____||_   _|| ____|   / \   |  \/  |
  / _ \  | |     | | |  \| ||  _|    | |  |  _|    / _ \  | |\/| |
 / ___ \ | |___  | | | |\  || |___   | |  | |___  / ___ \ | |  | |
/_/   \_\|_____||___||_| \_||_____|  |_|  |_____|/_/   \_\|_|  |_|
 =================================================================
`

// header is the logo with version and license; shown by init and version only, never by hooks.
func header() string {
	return indent(logo + fmt.Sprintf(" :: GitFolio :: %48s\n", "("+version+")") +
		" MIT License - Copyright (c) 2026 Alineteam Inc.\n")
}

// Credentials live in their own file with owner-only permissions. Nothing here is ever printed.
type Credentials struct {
	Token          string `json:"token,omitempty"` // aline.team CLI token (aln_cli_…), sent only in the Authorization header
	TokenExpiresAt string `json:"tokenExpiresAt,omitempty"`
	Email          string `json:"email,omitempty"` // account email, shown by login
}

func loadCredentials(dir string) (c Credentials, err error) {
	err = loadJSON(filepath.Join(dir, "credentials.json"), &c)
	return c, err
}

func saveCredentials(dir string, c Credentials) error {
	return saveJSON(filepath.Join(dir, "credentials.json"), c) // 0600
}

// cmdInit walks a new user through setup (DESIGN 7.2): login, the repositories to collect, the
// settings, then a first sync. Nothing after the login step runs until the user is logged in to
// aline.team. Every question defaults to the current setting, so running init again changes only
// what the user changes.
func cmdInit(dir string, args []string) error {
	lang := detectLang(os.Getenv)
	fmt.Print(header() + "\n" + indent(tr(lang, "policy")) + "\n")
	if interactive() {
		prompt(tr(lang, "pressEnter"))
	}
	if err := cmdLogin(dir); err != nil {
		return err
	}
	if creds, err := loadCredentials(dir); err != nil || creds.Token == "" {
		return err // sign-up declined: nothing is set up before login
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}

	// Repositories: where they are, whose commits count, which ones to collect.
	section(lang, "reposTitle")
	roots := askRoots(lang, cfg.Roots, args)
	if out, err := git(".", "config", "--global", "user.email"); err == nil && strings.TrimSpace(out) != "" {
		notice(fmt.Sprintf(tr(lang, "identityEmail"), strings.TrimSpace(out)))
	} else {
		notice(tr(lang, "identityMissing"))
	}
	if err := withLock(dir, func() error {
		c, err := loadConfig(dir)
		if err != nil {
			return err
		}
		c.Roots = roots
		return saveConfig(dir, c)
	}); err != nil {
		return err
	}
	chosen, err := chooseRepos(lang, dir, roots)
	if err != nil {
		return err
	}

	// Settings.
	section(lang, "settingsTitle")
	autosync := askYesNo(lang, "autosyncAsk", !cfg.AutoSyncOff)
	notice("\n" + tr(lang, "depsNotice"))
	deps := askYesNo(lang, "depsAsk", cfg.Deps)
	blank()
	when := askSchedule(lang, cfg.Schedule)
	blank()
	if err := withLock(dir, func() error { return applySettings(dir, autosync, deps) }); err != nil {
		return err
	}
	if when != cfg.Schedule {
		if err := withLock(dir, func() error { return setSchedule(dir, lang, when) }); err != nil {
			warn(lang, "failed", err) // the rest of the setup still stands
		}
	}
	for _, p := range chosen {
		if err := withLock(dir, func() error { return registerRepo(dir, p) }); err != nil {
			warn(lang, "repoFailed", tildePath(p), err)
		}
	}

	// Sync (the first one sends the history of the chosen repositories), then what was set up.
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	if len(repos) > 0 {
		section(lang, "syncTitle")
		if err := withLock(dir, func() error { return cmdSync(dir, nil) }); err != nil {
			warn(lang, "failed", err) // sent by the next push or sync
		}
	}
	if cfg, err = loadConfig(dir); err != nil {
		return err
	}
	onOff := func(b bool) string { return tr(lang, map[bool]string{true: "on", false: "off"}[b]) }
	daily := cmp.Or(cfg.Schedule, tr(lang, "off"))
	blank()
	say(lang, "initDone", len(repos), onOff(!cfg.AutoSyncOff), onOff(cfg.Deps), daily)
	return nil
}

// chooseRepos finds unregistered repositories with the user's commits under roots and asks which to
// collect. Nothing is chosen by default: company code is never collected unless the user picks it.
func chooseRepos(lang, dir string, roots []string) ([]string, error) {
	blank()
	say(lang, "searching")
	repos, err := loadRepos(dir)
	if err != nil {
		return nil, err
	}
	registered := map[string]bool{}
	for _, r := range repos {
		registered[r.Path] = true
	}
	type candidate struct {
		path, last string
		commits    int
	}
	var cands []candidate
	for _, p := range findRepos(roots, 5) {
		if n, last := ownCommits(p); n > 0 && !registered[p] {
			cands = append(cands, candidate{p, last, n})
		}
	}
	if len(cands) == 0 {
		say(lang, "noCandidates")
		return nil, nil
	}
	blank()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, c := range cands {
		fmt.Fprintf(w, "%s  %d\t%s\t%d\t%s\n", margin, i+1, tildePath(c.path), c.commits, c.last)
	}
	w.Flush()
	notice("\n" + tr(lang, "companyNotice"))
	var sel []int
	for interactive() && !stdinClosed {
		answer := prompt(tr(lang, "selectRepos"))
		if answer == "" {
			break
		}
		if sel, err = parseSelection(answer, len(cands)); err == nil {
			break
		}
		notice(fmt.Sprintf(tr(lang, "badSelection"), len(cands)))
	}
	if len(sel) == 0 {
		if stdinClosed || !interactive() {
			say(lang, "chooseLater")
		} else {
			say(lang, "noneSelected")
		}
		return nil, nil
	}
	var chosen []string
	for _, i := range sel {
		chosen = append(chosen, cands[i-1].path)
	}
	return chosen, nil
}

// applySettings saves the push-time sending and dependency choices. Turning dependency detection on asks
// about the files of repositories already registered; turning it off deletes what it collected.
// Callers hold the data lock.
func applySettings(dir string, autosync, deps bool) error {
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	was := cfg.Deps
	cfg.AutoSyncOff, cfg.Deps, cfg.DepsAsked = !autosync, deps, true
	if err := saveConfig(dir, cfg); err != nil {
		return err
	}
	switch {
	case deps && !was:
		repos, err := loadRepos(dir)
		if err != nil {
			return err
		}
		for i := range repos {
			if err := reviewAndRescan(dir, &repos[i]); err != nil {
				return err
			}
		}
		return saveRepos(dir, repos)
	case !deps && was:
		return cmdDeps(dir, []string{"off"})
	}
	return nil
}

// askYesNo asks a yes/no question; Enter keeps def.
func askYesNo(lang, key string, def bool) bool {
	hint := map[bool]string{true: " [Y/n] > ", false: " [y/N] > "}[def]
	for {
		switch a := strings.ToLower(prompt(tr(lang, key) + hint)); {
		case a == "" || stdinClosed:
			return def
		case a == "y" || a == "yes":
			return true
		case a == "n" || a == "no":
			return false
		}
		notice(tr(lang, "yesNoAgain"))
	}
}

// askSchedule asks for the daily sync time: Enter keeps the current one (none at first), off removes it.
func askSchedule(lang, current string) string {
	hint := tr(lang, "scheduleAskNew")
	if current != "" {
		hint = fmt.Sprintf(tr(lang, "scheduleAskKeep"), current)
	}
	for {
		a := strings.ToLower(prompt(tr(lang, "scheduleAsk") + hint))
		if a == "" || stdinClosed {
			return current
		}
		if a == "off" || a == "n" || a == "no" {
			return ""
		}
		if h, m, ok := parseHHMM(a); ok {
			return fmt.Sprintf("%02d:%02d", h, m)
		}
		notice(tr(lang, "scheduleFormat"))
	}
}

// rootHints are common places to keep code, relative to the home folder.
var rootHints = []string{"Code", "code", "dev", "Dev", "src", "projects", "Projects", "workspace", "Workspace",
	"repos", "git", "GitHub", "IdeaProjects", "Documents/Code", "Documents/GitHub", "Documents/Projects", "Documents/dev"}

// suggestRoots returns the rootHints that exist under home, once each even on case-insensitive disks.
func suggestRoots(home string) []string {
	var found []string
	var infos []os.FileInfo
	for _, h := range rootHints {
		p := filepath.Join(home, filepath.FromSlash(h))
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() || slices.ContainsFunc(infos, func(o os.FileInfo) bool { return os.SameFile(o, st) }) {
			continue
		}
		infos = append(infos, st)
		found = append(found, p)
	}
	return found
}

// askRoots asks where the user keeps repositories. Folders given on the command line win; otherwise
// the saved or suggested folders are offered, and the whole home folder is the last resort.
func askRoots(lang string, saved, args []string) []string {
	home, _ := os.UserHomeDir()
	if len(args) > 0 {
		var roots []string
		for _, a := range args {
			if p, err := filepath.Abs(expandHome(a)); err == nil {
				roots = append(roots, p)
			}
		}
		return roots
	}
	defaults := saved
	if len(defaults) == 0 {
		defaults = suggestRoots(home)
	}
	fallback := defaults
	if len(fallback) == 0 {
		fallback = []string{home}
	}
	if !interactive() {
		return fallback
	}
	q := tr(lang, "rootsAsk")
	if len(defaults) > 0 {
		var shown []string
		for _, d := range defaults {
			shown = append(shown, tildePath(d))
		}
		q += fmt.Sprintf(tr(lang, "rootsFound"), strings.Join(shown, ", "))
	}
	q += tr(lang, "rootsPrompt")
	for {
		blank()
		answer := prompt(q)
		if answer == "" {
			return fallback
		}
		var roots []string
		missing := ""
		for _, a := range strings.Split(answer, ",") {
			p, err := filepath.Abs(expandHome(strings.TrimSpace(a)))
			if st, e := os.Stat(p); err != nil || e != nil || !st.IsDir() {
				missing = strings.TrimSpace(a)
				break
			}
			roots = append(roots, p)
		}
		if missing == "" {
			return roots
		}
		notice(fmt.Sprintf(tr(lang, "rootMissing"), missing))
	}
}

func expandHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + p[len(home):]
	}
	return p
}

// findRepos walks each root up to maxDepth folders deep and returns the git top-level of every
// repository found, once per repository even when it has several worktrees. It never descends into
// a repository (nested repositories and submodules are not offered), hidden folders, Library,
// node_modules or vendor, and only looks at folder names, not file contents.
func findRepos(roots []string, maxDepth int) []string {
	seen := map[string]bool{}
	var repos []string
	for _, root := range roots {
		root = filepath.Clean(root)
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil // unreadable folders (e.g. macOS privacy prompts declined) are skipped
			}
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "Library" || name == "node_modules" || name == "vendor") {
				return fs.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				common, err := git(p, "rev-parse", "--path-format=absolute", "--git-common-dir")
				top, err2 := topLevel(p)
				if err == nil && err2 == nil && !seen[strings.TrimSpace(common)] {
					seen[strings.TrimSpace(common)] = true
					repos = append(repos, top)
				}
				return fs.SkipDir
			}
			if rel, err := filepath.Rel(root, p); err == nil && rel != "." && strings.Count(rel, string(filepath.Separator))+1 >= maxDepth {
				return fs.SkipDir
			}
			return nil
		})
	}
	return repos
}

// ownCommits counts the user's commits in repo over the same range scan reads, and returns the date
// of the latest one (YYYY-MM-DD).
func ownCommits(repo string) (n int, last string) {
	mine, err := myEmails(repo)
	if err != nil {
		return 0, ""
	}
	rng, err := logRange(repo)
	if err != nil || rng == nil {
		return 0, ""
	}
	out, err := git(repo, append([]string{"log", "--no-merges", "--format=%aE %as"}, rng...)...)
	if err != nil {
		return 0, ""
	}
	for _, line := range strings.Split(out, "\n") {
		if email, date, ok := strings.Cut(line, " "); ok && mine[strings.ToLower(email)] {
			n++
			last = max(last, date)
		}
	}
	return n, last
}
