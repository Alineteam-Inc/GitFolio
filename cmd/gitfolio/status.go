package main

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

// languages maps a file extension, or a whole file name, to the language its changed lines count for.
// Data, prose and lock files (JSON, YAML, Markdown, ...) count for none, as in GitHub's language bar.
var languages = map[string]string{
	".go": "Go", ".js": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript", ".jsx": "JavaScript",
	".ts": "TypeScript", ".tsx": "TypeScript", ".py": "Python", ".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin",
	".swift": "Swift", ".m": "Objective-C", ".mm": "Objective-C", ".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++",
	".cxx": "C++", ".hpp": "C++", ".cs": "C#", ".rb": "Ruby", ".php": "PHP", ".rs": "Rust", ".scala": "Scala",
	".dart": "Dart", ".lua": "Lua", ".r": "R", ".sh": "Shell", ".bash": "Shell", ".zsh": "Shell",
	".ps1": "PowerShell", ".sql": "SQL", ".html": "HTML", ".htm": "HTML", ".css": "CSS", ".scss": "SCSS",
	".sass": "SCSS", ".less": "Less", ".vue": "Vue", ".svelte": "Svelte", ".groovy": "Groovy", ".gradle": "Groovy",
	".tf": "HCL", ".ex": "Elixir", ".exs": "Elixir", ".erl": "Erlang", ".hs": "Haskell", ".clj": "Clojure",
	".fs": "F#", ".pl": "Perl", ".proto": "Protocol Buffers", "dockerfile": "Dockerfile", "makefile": "Makefile",
}

// languageOf is the language of a file path in the repository, "" for none.
func languageOf(file string) string {
	base := strings.ToLower(path.Base(file))
	if l, ok := languages[base]; ok {
		return l
	}
	return languages[path.Ext(base)]
}

// repoStatus is what GitFolio holds for one repository: the user's commits collected on this computer,
// how many of them aline.team has, and what they show of the stack.
type repoStatus struct {
	commits, sent, changes, created, add, del, ai int
	last                                          time.Time
	lines                                         map[string]int // changed lines per language
	agents                                        map[string]int // commits per AI agent
	deps                                          map[string][]string
}

func statusOf(r Repo, commits []Commit, deps []Dependency, st syncState) repoStatus {
	s := repoStatus{lines: map[string]int{}, agents: map[string]int{}, deps: map[string][]string{}}
	for _, c := range commits {
		if c.Repo != r.ID {
			continue
		}
		s.commits++
		if r.Namespace != "" && st.Commits[r.Provider+"/"+r.Namespace+"/"+c.Hash] != "" {
			s.sent++
		}
		if c.CreationType != "" && c.CreationType != "HUMAN" {
			s.ai++
		}
		for _, a := range c.AIAgents {
			s.agents[a]++
		}
		if t, err := time.Parse(time.RFC3339, c.Date); err == nil && t.After(s.last) {
			s.last = t
		}
		for _, f := range c.Files {
			s.changes++
			s.add += f.Add
			s.del += f.Del
			if f.Created {
				s.created++
			}
			if l := languageOf(f.Name); l != "" && f.Add+f.Del > 0 { // binary files change no lines
				s.lines[l] += f.Add + f.Del
			}
		}
	}
	for _, d := range deps {
		if d.Repo == r.ID {
			s.deps[d.Ecosystem] = append(s.deps[d.Ecosystem], strings.TrimSpace(d.Name+" "+d.Version))
		}
	}
	return s
}

// languageShares lists the languages by their share of changed lines, the largest first, at most n (0: all).
func (s repoStatus) languageShares(n int) string {
	total := 0
	for _, v := range s.lines {
		total += v
	}
	if total == 0 {
		return "-"
	}
	names := slices.SortedFunc(maps.Keys(s.lines), func(a, b string) int {
		return cmp.Or(cmp.Compare(s.lines[b], s.lines[a]), cmp.Compare(a, b))
	})
	if n > 0 && len(names) > n {
		names = names[:n]
	}
	parts := make([]string, len(names))
	for i, l := range names {
		pct := fmt.Sprintf("%d%%", (s.lines[l]*100+total/2)/total)
		if pct == "0%" {
			pct = "<1%"
		}
		parts[i] = l + " " + pct
	}
	return strings.Join(parts, ", ")
}

// depCounts is "npm 64, go 3", or "off" when dependency detection is off.
func (s repoStatus) depCounts(on bool) string {
	if !on {
		return "off"
	}
	if len(s.deps) == 0 {
		return "-"
	}
	var parts []string
	for _, eco := range slices.Sorted(maps.Keys(s.deps)) {
		parts = append(parts, fmt.Sprintf("%s %d", eco, len(s.deps[eco])))
	}
	return strings.Join(parts, ", ")
}

func (s repoStatus) aiShare() string {
	if s.commits == 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", (s.ai*100+s.commits/2)/s.commits)
}

func (s repoStatus) lastCommit() string {
	if s.last.IsZero() {
		return "-"
	}
	return s.last.Local().Format("2006-01-02")
}

// sentCount is how many of the commits aline.team has: "-" without a git service remote (never sent),
// "waiting" while aline.team takes the repository only after an email is verified.
func (s repoStatus) sentCount(r Repo, st syncState) string {
	switch {
	case r.Namespace == "":
		return "-"
	case st.Waiting[r.Provider+"/"+r.Namespace] != "":
		return "waiting"
	}
	return fmt.Sprint(s.sent)
}

// cmdStatus shows what GitFolio has collected, from this computer's data only (no request to aline.team):
// per repository with no argument, in full for one repository (its name or path).
func cmdStatus(dir string, args []string) error {
	lang := detectLang(os.Getenv)
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	commits, err := readCommits(dir)
	if err != nil {
		return err
	}
	deps, err := loadDeps(dir)
	if err != nil {
		return err
	}
	st, err := loadSync(dir)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		r, ok := findRepo(repos, args[0])
		if !ok {
			return failure("notRegistered", args[0])
		}
		s := statusOf(r, commits, deps, st)
		agents := "-"
		if len(s.agents) > 0 {
			var parts []string
			for _, a := range slices.Sorted(maps.Keys(s.agents)) {
				parts = append(parts, fmt.Sprintf("%s %d", a, s.agents[a]))
			}
			agents = strings.Join(parts, ", ")
		}
		where := cmp.Or(strings.TrimSpace(r.Provider+" "+r.Namespace), "-")
		sayKV(lang, "statusRepo", r.Name, where, s.commits, s.sentCount(r, st), s.changes, s.add, s.del, s.created,
			s.aiShare(), agents, s.languageShares(0), s.depCounts(cfg.Deps), s.lastCommit())
		if cfg.Deps {
			for _, eco := range slices.Sorted(maps.Keys(s.deps)) {
				names := slices.Sorted(slices.Values(s.deps[eco]))
				notice(fmt.Sprintf("%s (%d): %s\n", eco, len(names), strings.Join(names, ", ")))
			}
		}
		if key := r.Provider + "/" + r.Namespace; st.Waiting[key] != "" {
			waitingNotice(lang, map[string]string{key: st.Waiting[key]})
		}
		return nil
	}
	if len(repos) == 0 {
		say(lang, "statusNone")
		return nil
	}
	// A result paragraph like the others (DESIGN 7.1): the title after the mark, the table under it.
	// The table is aligned first and its header dimmed after, so color codes do not shift the columns.
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMMITS\tSENT\tFILE CHANGES\tLINES\tAI\tLANGUAGES\tDEPENDENCIES\tLAST COMMIT")
	for _, r := range repos {
		s := statusOf(r, commits, deps, st)
		fmt.Fprintf(w, "%s\t%d\t%s\t%d\t+%d -%d\t%s\t%s\t%s\t%s\n", r.Name, s.commits, s.sentCount(r, st), s.changes,
			s.add, s.del, s.aiShare(), s.languageShares(3), s.depCounts(cfg.Deps), s.lastCommit())
	}
	if err := w.Flush(); err != nil {
		return err
	}
	header, rows, _ := strings.Cut(b.String(), "\n")
	title := strings.TrimRight(fmt.Sprintf(tr(lang, "statusTitle"), len(repos)), "\n")
	show(out, title+"\n"+dim(strings.TrimRight(header, " "))+"\n"+rows)
	waitingNotice(lang, st.Waiting)
	notice(tr(lang, "statusMore"))
	return nil
}

// findRepo finds a registered repository by its name or by a path inside it.
func findRepo(repos []Repo, arg string) (Repo, bool) {
	if i := slices.IndexFunc(repos, func(r Repo) bool { return r.Name == arg }); i >= 0 {
		return repos[i], true
	}
	top, err := topLevel(arg)
	if err != nil {
		if top, err = filepath.Abs(arg); err != nil {
			return Repo{}, false
		}
	}
	i := slices.IndexFunc(repos, func(r Repo) bool { return r.Path == top })
	if i < 0 {
		return Repo{}, false
	}
	return repos[i], true
}
