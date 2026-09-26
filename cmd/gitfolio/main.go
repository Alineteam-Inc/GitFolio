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
  scan [path] [--all]   collect new commits (--all: every registered repository)
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
	n, err := scanRepo(dir, r)
	if err != nil {
		return err
	}
	fmt.Printf("registered %s (%d commits)\n", r.Name, n)
	return saveRepos(dir, repos)
}

func cmdScan(dir string, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	all := fs.Bool("all", false, "scan every registered repository")
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
		n, err := scanRepo(dir, r)
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

// cmdExport prints collected commits without local-only fields (hash, path).
func cmdExport(dir string) error {
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	commits, err := readCommits(dir)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, r := range repos {
		names[r.ID] = r.Name
	}
	type exported struct {
		Repo    string     `json:"repo"`
		Date    string     `json:"date"`
		Message string     `json:"message"`
		Files   []FileStat `json:"files"`
	}
	out := make([]exported, 0, len(commits))
	for _, c := range commits {
		out = append(out, exported{names[c.Repo], c.Date, c.Message, c.Files})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
