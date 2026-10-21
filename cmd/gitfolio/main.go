package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
)

// version is set at release time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage: gitfolio <command> [arguments]

commands:
  init [folder...]      first-time setup: data policy, aline.team log in / sign up, then find the repositories
                        under your code folders and choose which ones to collect
  login                 log in / sign up to aline.team with an email code (an email without an
                        account is signed up)
  logout                sign out: revoke and remove this device's token
  whoami                show the aline.team account this device is signed in to
  add [path]            register a repository, collect its commits and install git hooks
                        (post-commit, pre-push) so later pushes are collected automatically
  remove [path] [--purge]
                        unregister a repository and restore its previous hooks
                        (--purge: also delete its collected commits, here and on aline.team)
  scan [path] [--all] [--rebuild]
                        collect new commits (--all: every registered repository,
                        --rebuild: drop stored commits and collect again)
  sync [--dry-run]      collect new commits in every repository and send what aline.team
                        does not have yet (--dry-run: print it as JSON, send nothing).
                        Pushes are sent right after git push unless autosync is off;
                        in local mode (autosync off) only the statistics are sent
  list                  show registered repositories
  status [name|path]    what GitFolio collected per repository: commits, how many aline.team has,
                        file changes, lines, AI use, languages, dependencies (this computer's data only)
  stats [--refresh]     your developer type and its five scores from aline.team, saved here after the
                        first time (--refresh: get it again; aline.team makes it if it has none yet).
                        In local mode it sends the statistics first and gets the type each time
  stack                 how long you used each language, framework and library, from aline.team
                        (local mode: sends the statistics first)
  portfolio [file.md|file.pdf] [--from file.md]
                        write your portfolio from your git history: developer type, tech stack and
                        repositories, as Markdown to edit or a PDF printed with Chrome, Edge or
                        Chromium (no file named: it asks which); --from prints your edited Markdown
  mcp                   serve GitFolio to AI coding tools (Claude Code, Codex, ...) as an MCP server
                        on stdin and stdout; its tools only read (status, devtype, stack, portfolio)
  export                print collected commits, dependencies and statistics as JSON
  deps [on|off|review [path]]
                        dependency detection: show status, turn it on (you choose which
                        package manager files may be read) or off (collected dependencies
                        are deleted), or change the file choices of a repository
  deps scan [path] [--all]
                        read the approved package manager files again to update the
                        dependencies
  deps auto on|off      scan, sync and the daily sync also read the approved package
                        manager files (off by default; pushes never read them)
  config                show settings
  config mask add|rm <word>...
                        add or remove blocked words (customer or internal project names);
                        added words are also applied to already stored data
  config autosync on|off
                        send to aline.team right after each git push (on by default);
                        off is local mode: nothing is sent until stats, stack or sync, and
                        the daily sync is removed
  config git-history on|off
                        keep git's own record of the git commands gitfolio runs (on by default;
                        off deletes it)
  config ai [name <repo> real|anonymous|<name> | messages <repo> off|subjects|full |
            branches <repo> on|off | hide|show <repo>]
                        what AI coding tools see of each repository through gitfolio mcp: its real
                        name (default), anonymous or another name; commit messages and branch names
                        (off by default); or nothing. Paths and file names are never shown
  history [--all]       the git commands gitfolio ran, newest last (--all: every run kept)
  email [add <email>... | verify <email> | rm <email> | primary <email>]
                        your work emails: commits by any of them count as yours, and aline.team
                        gets all of them under the primary one (the first, marked ★); verified
                        ones (✔, email code) merge the repositories linked on aline.team with them
  schedule [HH:MM | off]
                        optional daily sync at a local time, for pushes the hooks missed
                        (launchd on macOS, systemd or cron on Linux); no argument: show it
  update                install the newest release now (otherwise GitFolio offers it once a day)
  version               print version`

func main() {
	if err := run(os.Args[1:]); err != nil {
		warn(detectLang(os.Getenv), "failed", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(usage)
		return nil
	}
	if args[0] == "version" {
		fmt.Print(header())
		return nil
	}
	if args[0] != "hook" && !slices.Contains(commands, args[0]) { // before anything else runs for a typo
		return unknownCommand(args[0])
	}
	if _, err := exec.LookPath("git"); err != nil {
		return failure("noGit")
	}
	dir, err := dataDir()
	if err != nil {
		return err
	}
	startHistory(dir, args)
	if args[0] == "mcp" { // stdout is the protocol: no update question or notices before it
		return cmdMCP(dir, args[1:])
	}
	if args[0] != "hook" && args[0] != "update" && checkUpdate(dir) {
		return nil // updated: the user runs the command again with the new binary
	}
	if !slices.Contains([]string{"hook", "init", "login", "logout", "email"}, args[0]) {
		checkEmails(dir, detectLang(os.Getenv))
		checkLocalMode(dir, detectLang(os.Getenv))
	}
	if collects(args) {
		c, err := newClient(dir) // only a login to the server this build talks to counts
		if err != nil {
			return err
		}
		if args[0] != "hook" {
			c.ensureLogin() // not logged in: a person at the terminal goes straight into logging in
		}
		if c.creds.Token == "" {
			if args[0] == "hook" {
				return nil // hooks do nothing before login
			}
			return failure("loginFirst")
		}
	}
	switch args[0] {
	case "hook":
		return cmdHook(dir, args[1:]) // takes the lock itself, only where it writes data
	case "init":
		return cmdInit(dir, args[1:]) // interactive; takes the lock only while writing
	case "login":
		return cmdLogin(dir)
	case "logout":
		return cmdLogout(dir)
	case "whoami":
		return cmdWhoami(dir)
	case "history":
		return cmdHistory(dir, args[1:]) // only reads
	case "update":
		return cmdUpdate(dir)
	case "stats":
		return cmdStats(dir, args[1:]) // outside the lock: it may wait on aline.team
	case "stack":
		return cmdStack(dir, args[1:]) // the same
	case "portfolio":
		return cmdPortfolio(dir, args[1:]) // the same; reads this computer's data under the lock
	}
	return withLock(dir, func() error {
		switch args[0] {
		case "add":
			return cmdAdd(dir, pathArg(args[1:]))
		case "remove":
			return cmdRemove(dir, args[1:])
		case "scan":
			return cmdScan(dir, args[1:])
		case "sync":
			return cmdSync(dir, args[1:])
		case "list":
			return cmdList(dir)
		case "status":
			return cmdStatus(dir, args[1:])
		case "export":
			return cmdExport(dir)
		case "config":
			return cmdConfig(dir, args[1:])
		case "deps":
			return cmdDeps(dir, args[1:])
		case "schedule":
			return cmdSchedule(dir, args[1:])
		case "email":
			return cmdEmail(dir, args[1:])
		}
		return unknownCommand(args[0])
	})
}

// commands are the commands people type; "hook" is run by git hooks only and never suggested.
var commands = []string{"init", "login", "logout", "whoami", "add", "remove", "scan", "sync", "list", "status",
	"stats", "stack", "portfolio", "mcp", "export", "config", "deps", "schedule", "email", "history", "update", "version", "help"}

// unknownCommand names the commands the user may have meant (similarCommands), or points to help.
func unknownCommand(s string) error {
	like := similarCommands(s)
	if len(like) == 0 {
		return failure("unknownCommand", s)
	}
	for i, c := range like {
		like[i] = "`gitfolio " + c + "`"
	}
	return failure("didYouMean", s, strings.Join(like, ", "))
}

// similarCommands are the commands s may have meant: version for -v and --version, the commands s
// starts (sta: status), then misspellings one or two edits away (stauts: status; one for short names).
func similarCommands(s string) []string {
	if s := strings.ToLower(strings.TrimLeft(s, "-")); s == "v" || s == "version" {
		return []string{"version"}
	}
	return similar(s, commands)
}

// similar are the words s may have meant: the ones s starts, then misspellings one or two edits away
// (one for short words). Flags are compared without their dashes (--dryrun: --dry-run).
func similar(s string, words []string) []string {
	s = strings.ToLower(strings.TrimLeft(s, "-"))
	bare := func(w string) string { return strings.TrimLeft(w, "-") }
	var prefix, close []string
	for _, w := range words {
		switch d := editDistance(s, bare(w)); {
		case len(s) >= 2 && strings.HasPrefix(bare(w), s):
			prefix = append(prefix, w)
		case d <= 1 || (d == 2 && len(bare(w)) > 4):
			close = append(close, w)
		}
	}
	slices.SortStableFunc(close, func(a, b string) int { return editDistance(s, bare(a)) - editDistance(s, bare(b)) }) // closest first
	return append(prefix, close...)
}

// wrongWord answers a word that cmd ("config autosync") does not take with the words the user may have
// meant from those it takes, else with its usage.
func wrongWord(cmd, got string, words []string, usage string) error {
	like := similar(got, words)
	if len(like) == 0 || slices.Contains(words, got) { // a right word in the wrong place
		return failure("usage", usage)
	}
	for i, w := range like {
		like[i] = "`gitfolio " + cmd + " " + w + "`"
	}
	return failure("didYouMeanWord", cmd, got, strings.Join(like, ", "))
}

// parseFlags is fs.Parse in GitFolio's words: an unknown flag is answered by wrongWord, nothing printed.
func parseFlags(fs *flag.FlagSet, args []string, usage string) error {
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	if err == nil {
		return nil
	}
	var flags []string
	fs.VisitAll(func(f *flag.Flag) { flags = append(flags, "--"+f.Name) })
	_, bad, _ := strings.Cut(err.Error(), "not defined: ") // "" for -h or a bad value: the usage
	// Shown as typed: "--dryrun", not the "-dryrun" the flag package reports.
	for _, a := range args {
		if name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "="); bad != "" && "-"+name == bad {
			bad = a
			break
		}
	}
	return wrongWord(fs.Name(), bad, flags, usage)
}

// editDistance counts the insertions, deletions, substitutions and swaps of neighbouring letters that
// turn a into b (optimal string alignment distance).
func editDistance(a, b string) int {
	x, y := []rune(a), []rune(b)
	d := make([][]int, len(x)+1)
	for i := range d {
		d[i] = make([]int, len(y)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(x); i++ {
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && x[i-1] == y[j-2] && x[i-2] == y[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(x)][len(y)]
}

// collects reports whether the command reads repositories. Nothing is collected before an aline.team
// login (DESIGN 6.1); commands that only show, clean up or configure keep working, so a logged-out
// user can still point to another server, remove repositories or turn dependency detection off.
func collects(args []string) bool {
	switch args[0] {
	case "add", "scan", "sync", "hook":
		return true
	case "deps":
		return len(args) > 1 && (args[1] == "on" || args[1] == "review" || args[1] == "scan")
	}
	return false
}

func pathArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

// flagsFirst moves "-x" arguments before the others so "scan . --all" works like "scan --all .".
func flagsFirst(args []string) []string {
	var flags, rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(flags, rest...)
}

func cmdAdd(dir, path string) error {
	top, err := topLevel(path)
	if err != nil {
		return err
	}
	n, err := registerRepo(dir, newRepo(top), true)
	if err == nil {
		say(detectLang(os.Getenv), "registered", filepath.Base(top), n)
	}
	return err
}

// registerRepo registers repo (a git top-level), collects it and installs hooks, and returns how many
// commits it collected; the caller reports it. With ask, it first asks which package manager files may
// be read; init asks for all chosen repositories at once instead.
// Callers hold the data lock.
func registerRepo(dir string, repo Repo, ask bool) (int, error) {
	top := repo.Path
	repos, err := loadRepos(dir)
	if err != nil {
		return 0, err
	}
	for _, r := range repos {
		if r.Path == top {
			return 0, failure("alreadyRegistered", tildePath(top))
		}
	}
	repos = append(repos, repo)
	r := &repos[len(repos)-1]
	cfg, err := loadConfig(dir)
	if err != nil {
		return 0, err
	}
	if len(cfg.Emails) == 0 { // the first repository's git email becomes the primary work email
		if out, err := git(top, "config", "user.email"); err == nil && len(addEmails(&cfg, []string{out})) == 0 {
			if err := saveConfig(dir, cfg); err != nil {
				return 0, err
			}
		}
	}
	if ask && cfg.Deps && interactive() {
		reviewManifests(repos[len(repos)-1:])
	}
	n, err := scanRepo(dir, r, false)
	if err != nil {
		return 0, err
	}
	if err := saveRepos(dir, repos); err != nil {
		return 0, err
	}
	if err := installHooks(top); err != nil {
		warn(detectLang(os.Getenv), "hooksNotInstalled", err)
	}
	return n, nil
}

func cmdRemove(dir string, args []string) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete this repository's collected commits")
	if err := parseFlags(fs, flagsFirst(args), "gitfolio remove [path] [--purge]"); err != nil {
		return err
	}
	p := pathArg(fs.Args())
	top, err := topLevel(p)
	if err != nil { // the folder may already be gone
		if top, err = filepath.Abs(p); err != nil {
			return err
		}
	}
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(repos, func(r Repo) bool { return r.Path == top })
	if i < 0 {
		return failure("notRegistered", tildePath(top))
	}
	r := repos[i]
	if err := uninstallHooks(top); err != nil {
		warn(detectLang(os.Getenv), "hooksNotRestored", err)
	}
	if *purge {
		commits, err := readCommits(dir)
		if err != nil {
			return err
		}
		if err := writeCommits(dir, slices.DeleteFunc(commits, func(c Commit) bool { return c.Repo == r.ID })); err != nil {
			return err
		}
		if err := queueDeletion(dir, deletion{r.Provider, r.Namespace}); err != nil {
			return err
		}
	}
	repos = slices.Delete(repos, i, i+1)
	if err := saveRepos(dir, repos); err != nil {
		return err
	}
	if len(repos) == 0 { // nothing left to sync: the daily sync goes too
		if cfg, err := loadConfig(dir); err == nil && cfg.Schedule != "" {
			if err := unschedule(dir); err != nil {
				return err
			}
			cfg.Schedule = ""
			if err := saveConfig(dir, cfg); err != nil {
				return err
			}
		}
	}
	say(detectLang(os.Getenv), "removed", r.Name)
	if *purge {
		say(detectLang(os.Getenv), "purgeQueued")
	}
	return nil
}

func cmdScan(dir string, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	all := fs.Bool("all", false, "scan every registered repository")
	rebuild := fs.Bool("rebuild", false, "drop stored commits and collect again")
	if err := parseFlags(fs, flagsFirst(args), "gitfolio scan [path] [--all] [--rebuild]"); err != nil {
		return err
	}
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	var top string
	if !*all {
		if top, err = topLevel(pathArg(fs.Args())); err != nil {
			return err
		}
	}
	lang := detectLang(os.Getenv)
	var failed, matched bool
	for i := range repos {
		r := &repos[i]
		if !*all && r.Path != top {
			continue
		}
		matched = true
		n, err := scanRepo(dir, r, *rebuild)
		if err != nil {
			warn(lang, "repoFailed", r.Name, err)
			failed = true
			continue
		}
		msg := fmt.Sprintf(tr(lang, "scanned"), r.Name, n)
		if cfg.Deps && cfg.DepsAuto {
			if _, _, err := refreshDeps(dir, r); err != nil {
				warn(lang, "repoFailed", r.Name, err)
				failed = true
			}
		}
		if cfg.Deps {
			if n := pendingManifests(*r); n > 0 {
				msg += fmt.Sprintf(tr(lang, "depsPending"), n, r.Path)
			}
		}
		show(out, msg)
	}
	if !*all && !matched {
		return failure("notRegisteredAdd", tildePath(top))
	}
	if err := saveRepos(dir, repos); err != nil {
		return err
	}
	if failed {
		return failure("scanFailed")
	}
	return nil
}

func cmdList(dir string) error {
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	commits, err := readCommits(dir)
	if err != nil {
		return err
	}
	count := map[string]int{}
	for _, c := range commits {
		count[c.Repo]++
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMMITS\tHOOKS\tDEPS\tLAST SCAN\tPATH")
	for _, r := range repos {
		deps := "off"
		if cfg.Deps {
			allowed := 0
			for _, ok := range r.Manifests {
				if ok {
					allowed++
				}
			}
			deps = fmt.Sprintf("%d files", allowed)
			if n := pendingManifests(r); n > 0 {
				deps += fmt.Sprintf(" (%d to review)", n)
			}
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n", r.Name, count[r.ID], hookStatus(r.Path), deps, r.LastScan, r.Path)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	st, err := loadSync(dir)
	if err != nil {
		return err
	}
	waitingNotice(detectLang(os.Getenv), st.Waiting) // also after pushes, which send in the background
	return nil
}

type exportData struct {
	Commits      []Commit     `json:"commits"`
	Dependencies []Dependency `json:"dependencies"`
	Snapshots    []snapshot   `json:"snapshots"` // what aline.team gets of each repository (snapshot.go)
}

// buildExport assembles what leaves the computer: repository names and namespaces instead of local IDs,
// only the dependencies of modules the user's own commits touched, and the snapshots. Module IDs stay local.
func buildExport(dir string) (exportData, error) {
	cfg, err := loadConfig(dir)
	if err != nil {
		return exportData{}, err
	}
	repos, err := loadRepos(dir)
	if err != nil {
		return exportData{}, err
	}
	commits, err := readCommits(dir)
	if err != nil {
		return exportData{}, err
	}
	deps, err := loadDeps(dir)
	if err != nil {
		return exportData{}, err
	}
	byID := map[string]Repo{}
	for _, r := range repos {
		byID[r.ID] = r
	}
	out := exportData{make([]Commit, 0, len(commits)), make([]Dependency, 0, len(deps)), snapshots(repos, commits, primaryEmail(cfg))}
	touched := map[string]bool{} // repo ID + "/" + module ID
	for _, c := range commits {
		for i := range c.Files { // commits is this function's own copy: module IDs are cleared in place
			if c.Files[i].Module != "" {
				touched[c.Repo+"/"+c.Files[i].Module] = true
			}
			c.Files[i].Module = ""
		}
		r := byID[c.Repo]
		c.Repo, c.Provider, c.Namespace = r.Name, r.Provider, r.Namespace
		out.Commits = append(out.Commits, c)
	}
	seen := map[string]bool{}
	for _, d := range deps {
		key := d.Repo + "/" + d.Ecosystem + "/" + d.Name
		if !touched[d.Repo+"/"+d.Module] || seen[key] {
			continue
		}
		seen[key] = true
		r := byID[d.Repo]
		d.Repo, d.Provider, d.Namespace, d.Module = r.Name, r.Provider, r.Namespace, ""
		out.Dependencies = append(out.Dependencies, d)
	}
	return out, nil
}

func cmdExport(dir string) error {
	out, err := buildExport(dir)
	if err != nil {
		return err
	}
	return printJSON(out)
}

// printJSON prints v indented, keeping "<a@b.com>" readable instead of <…>.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func cmdConfig(dir string, args []string) error {
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return printJSON(cfg)
	}
	if args[0] == "autosync" && len(args) == 2 && (args[1] == "on" || args[1] == "off") {
		cfg.AutoSyncOff, cfg.LocalModeKnown = args[1] == "off", true
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		lang := detectLang(os.Getenv)
		say(lang, "autosync", tr(lang, map[bool]string{true: "syncAuto", false: "syncLocal"}[!cfg.AutoSyncOff]))
		if cfg.AutoSyncOff && cfg.Schedule != "" { // local mode sends nothing by itself: no daily sync either
			return setSchedule(dir, lang, "")
		}
		return nil
	}
	if args[0] == "git-history" && len(args) == 2 && (args[1] == "on" || args[1] == "off") {
		cfg.GitHistoryOff = args[1] == "off"
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		lang := detectLang(os.Getenv)
		if cfg.GitHistoryOff {
			gitTrace = "" // nothing more is written by this run
			if err := os.Remove(filepath.Join(dir, historyFile)); err != nil && !os.IsNotExist(err) {
				return err
			}
			say(lang, "historyTurnedOff")
			return nil
		}
		say(lang, "historyTurnedOn")
		return nil
	}
	// Development only: not in the help, and release builds refuse it (apiBase ignores it there too).
	if args[0] == "api-url" && version == "dev" { // aline.team server, e.g. the development server; "default" = production
		cfg.APIURL = ""
		if len(args) > 1 && args[1] != "default" {
			if err := checkAPIURL(args[1]); err != nil {
				return err
			}
			cfg.APIURL = strings.TrimRight(args[1], "/")
		}
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		base, err := apiBase(cfg.APIURL)
		if err != nil {
			return err
		}
		say(detectLang(os.Getenv), "apiURL", base)
		return nil
	}
	if args[0] == "ai" {
		return cmdConfigAI(dir, args[1:])
	}
	if len(args) < 3 || args[0] != "mask" || (args[1] != "add" && args[1] != "rm") {
		const usage = "gitfolio config mask add|rm <word>... | config autosync on|off | config git-history on|off | config ai ..."
		switch {
		case !slices.Contains([]string{"mask", "autosync", "git-history", "ai"}, args[0]):
			return wrongWord("config", args[0], []string{"mask", "autosync", "git-history", "ai"}, usage)
		case len(args) > 1 && args[0] == "mask":
			return wrongWord("config mask", args[1], []string{"add", "rm"}, usage)
		case len(args) > 1:
			return wrongWord("config "+args[0], args[1], []string{"on", "off"}, usage)
		}
		return failure("usage", usage)
	}
	for _, w := range args[2:] {
		if w = strings.TrimSpace(w); w == "" {
			continue
		}
		i := slices.IndexFunc(cfg.Mask, func(x string) bool { return strings.EqualFold(x, w) })
		switch {
		case args[1] == "add" && i < 0:
			cfg.Mask = append(cfg.Mask, w)
		case args[1] == "rm" && i >= 0:
			cfg.Mask = slices.Delete(cfg.Mask, i, i+1)
		}
	}
	if err := saveConfig(dir, cfg); err != nil {
		return err
	}
	if args[1] == "rm" {
		say(detectLang(os.Getenv), "maskRemoved")
		return nil
	}
	return remask(dir, newMasker(cfg.Mask))
}

// remask applies m to the stored commit messages, so a newly blocked word disappears from past data too.
func remask(dir string, m masker) error {
	commits, err := readCommits(dir)
	if err != nil {
		return err
	}
	for i := range commits {
		m.commit(&commits[i])
	}
	if err := writeCommits(dir, commits); err != nil {
		return err
	}
	say(detectLang(os.Getenv), "maskApplied", len(commits))
	return nil
}
