package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
  init [folder...]      first-time setup: data policy, aline.team login, then find the repositories
                        under your code folders and choose which ones to collect
  login                 sign in or sign up to aline.team with an email code; registers this device
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
                        Pushes are sent right after git push unless autosync is off
  list                  show registered repositories
  export                print collected commits as JSON
  deps [on|off|review [path]]
                        dependency detection: show status, turn it on (you choose which
                        package manager files may be read) or off (collected dependencies
                        are deleted), or change the file choices of a repository
  config                show settings
  config mask add|rm <word>...
                        add or remove blocked words (customer or internal project names);
                        added words are also applied to already stored data
  config api-url <url>|default
                        aline.team server to use (e.g. the development server);
                        the GITFOLIO_API_URL environment variable overrides it
  config autosync on|off
                        send to aline.team right after each git push (on by default)
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
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git not found in PATH")
	}
	dir, err := dataDir()
	if err != nil {
		return err
	}
	if collects(args) {
		creds, err := loadCredentials(dir)
		if err != nil {
			return err
		}
		if creds.Token == "" {
			if args[0] == "hook" {
				return nil // hooks do nothing before login
			}
			return errors.New(strings.TrimSpace(tr(detectLang(os.Getenv), "loginFirst")))
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
		case "export":
			return cmdExport(dir)
		case "config":
			return cmdConfig(dir, args[1:])
		case "deps":
			return cmdDeps(dir, args[1:])
		}
		return fmt.Errorf("unknown command %q (see gitfolio help)", args[0])
	})
}

// collects reports whether the command reads repositories. Nothing is collected before an aline.team
// login (DESIGN 6.1); commands that only show, clean up or configure keep working, so a logged-out
// user can still point to another server, remove repositories or turn dependency detection off.
func collects(args []string) bool {
	switch args[0] {
	case "add", "scan", "sync", "hook":
		return true
	case "deps":
		return len(args) > 1 && (args[1] == "on" || args[1] == "review")
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
	return registerRepo(dir, top)
}

// registerRepo registers the repository at top (its git top-level), collects it and installs hooks.
// Callers hold the data lock.
func registerRepo(dir, top string) error {
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	for _, r := range repos {
		if r.Path == top {
			return fmt.Errorf("%s is already registered", top)
		}
	}
	repos = append(repos, newRepo(top))
	r := &repos[len(repos)-1]
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if cfg.Deps && interactive() {
		if _, err := reviewManifests(r); err != nil {
			return err
		}
	}
	n, err := scanRepo(dir, r, false)
	if err != nil {
		return err
	}
	if err := saveRepos(dir, repos); err != nil {
		return err
	}
	lang := detectLang(os.Getenv)
	say(lang, "registered", r.Name, n)
	if err := installHooks(top); err != nil {
		warn(lang, "hooksNotInstalled", err)
	}
	return nil
}

func cmdRemove(dir string, args []string) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete this repository's collected commits")
	if err := fs.Parse(flagsFirst(args)); err != nil {
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
		return fmt.Errorf("%s is not registered", top)
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
		if err := queueDeletion(dir, deletion{What: "repository", Provider: r.Provider, Namespace: r.Namespace}); err != nil {
			return err
		}
	}
	if err := saveRepos(dir, slices.Delete(repos, i, i+1)); err != nil {
		return err
	}
	say(detectLang(os.Getenv), "removed", r.Name)
	return nil
}

func cmdScan(dir string, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	all := fs.Bool("all", false, "scan every registered repository")
	rebuild := fs.Bool("rebuild", false, "drop stored commits and collect again")
	if err := fs.Parse(flagsFirst(args)); err != nil {
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
		if cfg.Deps {
			if n := pendingManifests(*r); n > 0 {
				msg += fmt.Sprintf(tr(lang, "depsPending"), n, r.Path)
			}
		}
		show(os.Stdout, msg)
	}
	if !*all && !matched {
		return fmt.Errorf("%s is not registered (run: gitfolio add)", top)
	}
	if err := saveRepos(dir, repos); err != nil {
		return err
	}
	if failed {
		return errors.New("some repositories failed to scan")
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
	return w.Flush()
}

type exportData struct {
	Commits      []Commit     `json:"commits"`
	Dependencies []Dependency `json:"dependencies"`
}

// buildExport assembles what leaves the computer: repository names and namespaces instead of local IDs,
// and only the dependencies of modules the user's own commits touched. Module IDs stay local.
func buildExport(dir string) (exportData, error) {
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
	out := exportData{make([]Commit, 0, len(commits)), make([]Dependency, 0, len(deps))}
	touched := map[string]bool{} // repo ID + "/" + module ID
	for _, c := range commits {
		files := slices.Clone(c.Files)
		for i := range files {
			if files[i].Module != "" {
				touched[c.Repo+"/"+files[i].Module] = true
			}
			files[i].Module = ""
		}
		r := byID[c.Repo]
		c.Repo, c.Provider, c.Namespace, c.Files = r.Name, r.Provider, r.Namespace, files
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
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // keep "<a@b.com>" readable instead of <…>
	return enc.Encode(out)
}

func cmdConfig(dir string, args []string) error {
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(cfg)
	}
	if args[0] == "autosync" && len(args) == 2 && (args[1] == "on" || args[1] == "off") {
		cfg.AutoSyncOff = args[1] == "off"
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		lang := detectLang(os.Getenv)
		say(lang, "autosync", tr(lang, args[1]))
		return nil
	}
	if args[0] == "api-url" { // aline.team server, e.g. the development server; "default" = production
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
	if len(args) < 3 || args[0] != "mask" || (args[1] != "add" && args[1] != "rm") {
		return errors.New("usage: gitfolio config mask add|rm <word>... | config api-url <url>|default | config autosync on|off")
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

// remask applies m to everything already stored, so a newly blocked word disappears from past data too.
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
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	for i := range repos {
		repos[i].Name = m.apply(filepath.Base(repos[i].Path))
	}
	if err := saveRepos(dir, repos); err != nil {
		return err
	}
	say(detectLang(os.Getenv), "maskApplied", len(commits))
	return nil
}
