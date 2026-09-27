package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"text/tabwriter"
)

// version is set at release time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage: gitfolio <command> [arguments]

commands:
  add [path]            register a repository and collect its commits
  scan [path] [--all] [--rebuild]
                        collect new commits (--all: every registered repository,
                        --rebuild: drop stored commits and collect again)
  list                  show registered repositories
  export                print collected commits as JSON
  version               print version`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gitfolio:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(usage)
		return nil
	}
	if args[0] == "version" {
		fmt.Println("gitfolio", version)
		return nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git not found in PATH")
	}
	dir, err := dataDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		return cmdAdd(dir, pathArg(args[1:]))
	case "scan":
		return cmdScan(dir, args[1:])
	case "list":
		return cmdList(dir)
	case "export":
		return cmdExport(dir)
	}
	return fmt.Errorf("unknown command %q (see gitfolio help)", args[0])
}

func pathArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

func cmdAdd(dir, path string) error {
	top, err := topLevel(path)
	if err != nil {
		return err
	}
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
	n, err := scanRepo(dir, r, false)
	if err != nil {
		return err
	}
	fmt.Printf("registered %s (%d commits)\n", r.Name, n)
	return saveRepos(dir, repos)
}

func cmdScan(dir string, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	all := fs.Bool("all", false, "scan every registered repository")
	rebuild := fs.Bool("rebuild", false, "drop stored commits and collect again")
	if err := fs.Parse(args); err != nil {
		return err
	}
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	var top string
	if !*all {
		if top, err = topLevel(pathArg(fs.Args())); err != nil {
			return err
		}
	}
	var failed, matched bool
	for i := range repos {
		r := &repos[i]
		if !*all && r.Path != top {
			continue
		}
		matched = true
		n, err := scanRepo(dir, r, *rebuild)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gitfolio: %s: %v\n", r.Name, err)
			failed = true
			continue
		}
		fmt.Printf("%s: %d new commits\n", r.Name, n)
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
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMMITS\tLAST SCAN\tPATH")
	for _, r := range repos {
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", r.Name, count[r.ID], r.LastScan, r.Path)
	}
	return w.Flush()
}

// cmdExport prints collected commits with the repository name and namespace in place of the local repo ID.
func cmdExport(dir string) error {
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	commits, err := readCommits(dir)
	if err != nil {
		return err
	}
	byID := map[string]Repo{}
	for _, r := range repos {
		byID[r.ID] = r
	}
	out := make([]Commit, 0, len(commits))
	for _, c := range commits {
		r := byID[c.Repo]
		c.Repo, c.Provider, c.Namespace = r.Name, r.Provider, r.Namespace
		out = append(out, c)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // keep "<a@b.com>" readable instead of <…>
	return enc.Encode(out)
}
