package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// manifestNames maps the supported package manager files to their ecosystem (DESIGN 3.5).
var manifestNames = map[string]string{
	"package.json":       "npm",
	"go.mod":             "go",
	"requirements.txt":   "pypi",
	"pyproject.toml":     "pypi",
	"pom.xml":            "maven",
	"build.gradle":       "maven",
	"build.gradle.kts":   "maven",
	"libs.versions.toml": "maven",
	"Cargo.toml":         "cargo",
}

// skipDirs hold other people's code or samples; manifest files inside are not even offered.
var skipDirs = []string{"node_modules", "vendor", "third_party", "examples", "testdata", "fixtures"}

const maxManifests = 200

// Dependency is one declared dependency of an approved manifest file.
type Dependency struct {
	Repo      string `json:"repo"`                // Repo.ID locally, repository name in export
	Provider  string `json:"provider,omitempty"`  // filled in export only
	Namespace string `json:"namespace,omitempty"` // filled in export only
	Module    string `json:"module,omitempty"`    // local only: matches FileStat.Module, never exported
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
}

type dep struct{ name, version string }

func loadDeps(dir string) (deps []Dependency, err error) {
	err = loadJSON(filepath.Join(dir, "deps.json"), &deps)
	return deps, err
}

// manifestCandidates lists supported manifest files committed at HEAD. It looks at file names only.
func manifestCandidates(repo string) []string {
	out, err := git(repo, "ls-tree", "-r", "-z", "--name-only", "HEAD")
	if err != nil {
		return nil // no commits yet
	}
	var files []string
	for _, p := range strings.Split(out, "\x00") {
		if _, ok := manifestNames[path.Base(p)]; !ok {
			continue
		}
		if slices.ContainsFunc(strings.Split(path.Dir(p), "/"), func(d string) bool { return slices.Contains(skipDirs, d) }) {
			continue
		}
		if len(files) == maxManifests {
			fmt.Fprintf(os.Stderr, "gitfolio: more than %d package manager files; the rest are ignored\n", maxManifests)
			break
		}
		files = append(files, p)
	}
	return files
}

// pendingManifests counts manifest files the user has not decided on yet.
func pendingManifests(r Repo) int {
	n := 0
	for _, p := range manifestCandidates(r.Path) {
		if _, decided := r.Manifests[p]; !decided {
			n++
		}
	}
	return n
}

func moduleID(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return hex.EncodeToString(sum[:4])
}

// moduleOf returns the module of the nearest directory above p that holds an approved manifest.
func moduleOf(p string, modules map[string]string) string {
	if len(modules) == 0 {
		return ""
	}
	for d := path.Dir(p); ; d = path.Dir(d) {
		if d == "." {
			d = ""
		}
		if id, ok := modules[d]; ok {
			return id
		}
		if d == "" {
			return ""
		}
	}
}

// refreshDeps reads only the approved manifest files of r (committed version, file contents are
// parsed in memory and dropped), stores their dependencies and returns the module IDs by directory.
// ponytail: re-reads every approved file on each scan; skip unchanged blobs if repos get many manifests.
func refreshDeps(dir string, r *Repo, m masker) (map[string]string, error) {
	modules := map[string]string{}
	var found []Dependency
	for p, ok := range r.Manifests {
		if !ok {
			continue
		}
		content, err := git(r.Path, "cat-file", "-p", "HEAD:"+p)
		if err != nil {
			continue // removed since it was approved
		}
		d := path.Dir(p)
		if d == "." {
			d = ""
		}
		modules[d] = moduleID(d)
		for _, x := range parseManifest(path.Base(p), content) {
			found = append(found, Dependency{Repo: r.ID, Module: modules[d], Ecosystem: manifestNames[path.Base(p)], Name: m.apply(x.name), Version: x.version})
		}
	}
	all, err := loadDeps(dir)
	if err != nil {
		return nil, err
	}
	all = append(slices.DeleteFunc(all, func(x Dependency) bool { return x.Repo == r.ID }), found...)
	slices.SortFunc(all, func(a, b Dependency) int {
		return strings.Compare(a.Repo+a.Module+a.Ecosystem+a.Name, b.Repo+b.Module+b.Ecosystem+b.Name)
	})
	return modules, saveJSON(filepath.Join(dir, "deps.json"), all)
}

// parseManifest extracts directly declared dependencies. Lock files, local paths, git and URL
// dependencies are left out.
func parseManifest(name, s string) []dep {
	var deps []dep
	switch name {
	case "package.json":
		deps = parsePackageJSON(s)
	case "go.mod":
		deps = parseGoMod(s)
	case "requirements.txt":
		for _, line := range strings.Split(s, "\n") {
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = line[:i]
			}
			if d, ok := pep508(line); ok {
				deps = append(deps, d)
			}
		}
	case "pyproject.toml":
		deps = parsePyproject(s)
	case "pom.xml":
		deps = parsePom(s)
	case "build.gradle", "build.gradle.kts":
		for _, g := range gradleDep.FindAllStringSubmatch(s, -1) {
			deps = append(deps, dep{g[1] + ":" + g[2], g[3]})
		}
	case "libs.versions.toml":
		deps = parseVersionCatalog(s)
	case "Cargo.toml":
		deps = parseCargo(s)
	}
	slices.SortFunc(deps, func(a, b dep) int { return strings.Compare(a.name, b.name) })
	return slices.CompactFunc(deps, func(a, b dep) bool { return a.name == b.name })
}

func parsePackageJSON(s string) []dep {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(s), &pkg) != nil {
		return nil
	}
	var deps []dep
	for _, m := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for n, v := range m {
			if !npmLocal(v) {
				deps = append(deps, dep{n, v})
			}
		}
	}
	return deps
}

// npmLocal reports npm versions that point at local paths, workspaces, git or URLs instead of the registry.
func npmLocal(v string) bool {
	for _, p := range []string{"file:", "link:", "workspace:", "portal:", "git", "http:", "https:", "github:", ".", "/", "~/"} {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return strings.Contains(v, "/") && !strings.HasPrefix(v, "npm:") // "user/repo" GitHub shorthand
}

func parseGoMod(s string) []dep {
	var deps []dep
	block := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			block = true
			continue
		case block && line == ")":
			block = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimPrefix(line, "require ")
		case !block:
			continue
		}
		if f := strings.Fields(line); len(f) >= 2 && !strings.Contains(line, "// indirect") {
			deps = append(deps, dep{f[0], f[1]})
		}
	}
	return deps
}

// pep508 splits a Python requirement such as `requests[socks]>=2.31; python_version>"3.8"`.
func pep508(s string) (dep, bool) {
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") ||
		strings.Contains(s, "://") || strings.Contains(s, " @ ") {
		return dep{}, false // options, local paths and URLs
	}
	name, version := s, ""
	if i := strings.IndexAny(s, "=<>!~[ "); i >= 0 {
		name = s[:i]
		if j := strings.IndexAny(s, "=<>!~"); j >= 0 {
			version = strings.TrimPrefix(strings.TrimSpace(s[j:]), "==")
		}
	}
	return dep{name, version}, name != ""
}

var quotedRe = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)

func quoted(s string) []string {
	var out []string
	for _, q := range quotedRe.FindAllStringSubmatch(s, -1) {
		out = append(out, q[1]+q[2])
	}
	return out
}

// tableKey returns key's string value in a TOML inline table such as `{ version = "1", path = "x" }`.
func tableKey(v, key string) string {
	if m := regexp.MustCompile(`(?:^|[\s{,])` + regexp.QuoteMeta(key) + `\s*=\s*"([^"]*)"`).FindStringSubmatch(v); m != nil {
		return m[1]
	}
	return ""
}

// tomlDep reads `name = "1.0"` or `name = { version = "1.0" }`; path and git dependencies are skipped.
func tomlDep(line string) (dep, bool) {
	k, v, ok := strings.Cut(line, "=")
	if !ok || strings.HasPrefix(line, "#") {
		return dep{}, false
	}
	name := strings.Trim(strings.TrimSpace(k), `"'`)
	name, _, _ = strings.Cut(name, ".") // serde.workspace = true
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "{") {
		if tableKey(v, "path") != "" || tableKey(v, "git") != "" {
			return dep{}, false
		}
		return dep{name, tableKey(v, "version")}, name != ""
	}
	version := ""
	if q := quoted(v); len(q) > 0 {
		version = q[0]
	}
	return dep{name, version}, name != ""
}

func parsePyproject(s string) []dep {
	var deps []dep
	section, inList := "", false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if !inList && section == "project" && strings.HasPrefix(t, "dependencies") {
			_, t, _ = strings.Cut(t, "=")
			inList = true
		}
		if inList {
			for _, q := range quoted(t) {
				if d, ok := pep508(q); ok {
					deps = append(deps, d)
				}
			}
			if strings.Contains(quotedRe.ReplaceAllString(t, ""), "]") {
				inList = false
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			section = strings.Trim(t, "[] ")
			continue
		}
		poetry := section == "tool.poetry.dependencies" ||
			strings.HasPrefix(section, "tool.poetry.group.") && strings.HasSuffix(section, ".dependencies")
		if d, ok := tomlDep(t); poetry && ok && d.name != "python" {
			deps = append(deps, d)
		}
	}
	return deps
}

func parseCargo(s string) []dep {
	isDeps := func(section string) bool {
		last := section[strings.LastIndexByte(section, '.')+1:]
		return last == "dependencies" || last == "dev-dependencies" || last == "build-dependencies"
	}
	var deps []dep
	var table *dep // [dependencies.serde] describes one dependency in the lines below
	skip, section := false, ""
	flush := func() {
		if table != nil && !skip {
			deps = append(deps, *table)
		}
		table, skip = nil, false
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			flush()
			section = strings.Trim(t, "[] ")
			if i := strings.LastIndexByte(section, '.'); i > 0 && isDeps(section[:i]) {
				table = &dep{name: section[i+1:]}
			}
			continue
		}
		if table != nil {
			if k, v, ok := strings.Cut(t, "="); ok {
				switch strings.TrimSpace(k) {
				case "version":
					if q := quoted(v); len(q) > 0 {
						table.version = q[0]
					}
				case "path", "git":
					skip = true
				}
			}
			continue
		}
		if d, ok := tomlDep(t); ok && isDeps(section) {
			deps = append(deps, d)
		}
	}
	flush()
	return deps
}

func parsePom(s string) []dep {
	var pom struct {
		Deps []struct {
			Group    string `xml:"groupId"`
			Artifact string `xml:"artifactId"`
			Version  string `xml:"version"`
		} `xml:"dependencies>dependency"`
	}
	if xml.Unmarshal([]byte(s), &pom) != nil {
		return nil
	}
	var deps []dep
	for _, d := range pom.Deps {
		deps = append(deps, dep{d.Group + ":" + d.Artifact, d.Version})
	}
	return deps
}

var gradleDep = regexp.MustCompile(`\b(?:implementation|api|compileOnly|runtimeOnly|testImplementation|testRuntimeOnly|annotationProcessor|kapt|ksp|classpath|compile|testCompile)\s*\(?\s*(?:platform\s*\(\s*)?["']([^"':\s]+):([^"':\s]+)(?::([^"'\s]+))?["']`)

func parseVersionCatalog(s string) []dep {
	versions := map[string]string{}
	type lib struct{ name, version, ref string }
	var libs []lib
	section := ""
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			section = strings.Trim(t, "[] ")
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok || strings.HasPrefix(t, "#") {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case section == "versions":
			if q := quoted(v); len(q) > 0 {
				versions[k] = q[0]
			}
		case section == "libraries" && strings.HasPrefix(v, "{"):
			name := tableKey(v, "module")
			if g := tableKey(v, "group"); name == "" && g != "" {
				name = g + ":" + tableKey(v, "name")
			}
			libs = append(libs, lib{name, tableKey(v, "version"), tableKey(v, "version.ref")})
		case section == "libraries":
			if q := quoted(v); len(q) > 0 {
				parts := strings.SplitN(q[0], ":", 3)
				if len(parts) == 3 {
					libs = append(libs, lib{parts[0] + ":" + parts[1], parts[2], ""})
				} else {
					libs = append(libs, lib{q[0], "", ""})
				}
			}
		}
	}
	var deps []dep
	for _, l := range libs {
		if l.version == "" {
			l.version = versions[l.ref]
		}
		if l.name != "" {
			deps = append(deps, dep{l.name, l.version})
		}
	}
	return deps
}

var stdin = bufio.NewReader(os.Stdin)

// interactive reports whether a person can answer prompts (not a hook, pipe or scheduled run).
func interactive() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func prompt(q string) string {
	fmt.Print(q)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

// parseSelection turns "all", "none" or "1,3,5-7" into sorted numbers between 1 and n.
func parseSelection(s string, n int) ([]int, error) {
	switch s = strings.ToLower(strings.TrimSpace(s)); s {
	case "all":
		sel := make([]int, n)
		for i := range sel {
			sel[i] = i + 1
		}
		return sel, nil
	case "none":
		return nil, nil
	}
	var sel []int
	for _, part := range strings.Split(s, ",") {
		lo, hi, isRange := strings.Cut(strings.TrimSpace(part), "-")
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b, err2 := a, error(nil)
		if isRange {
			b, err2 = strconv.Atoi(strings.TrimSpace(hi))
		}
		if err1 != nil || err2 != nil || a < 1 || b > n || a > b {
			return nil, fmt.Errorf("%q is not a valid choice (use all, none, or numbers like 1,3,5-7 between 1 and %d)", part, n)
		}
		for i := a; i <= b; i++ {
			sel = append(sel, i)
		}
	}
	slices.Sort(sel)
	return slices.Compact(sel), nil
}

// reviewManifests asks which of r's package manager files may be read. It returns false when
// the user kept the current decisions.
func reviewManifests(r *Repo) (bool, error) {
	files := manifestCandidates(r.Path)
	if len(files) == 0 {
		fmt.Printf("%s: no package manager files found\n", r.Name)
		return false, nil
	}
	fmt.Printf("Package manager files in %q. Selected files are read only to detect dependencies:\n", r.Name)
	for i, f := range files {
		state := "new"
		if ok, decided := r.Manifests[f]; decided {
			state = map[bool]string{true: "allowed", false: "declined"}[ok]
		}
		fmt.Printf("  %3d  %-8s  %s\n", i+1, state, f)
	}
	for {
		answer := prompt("Allow reading which files? [all / none / 1,3,5-7 / Enter = keep as is] > ")
		if answer == "" {
			return false, nil
		}
		sel, err := parseSelection(answer, len(files))
		if err != nil {
			fmt.Println(err)
			continue
		}
		r.Manifests = map[string]bool{}
		for i, f := range files {
			r.Manifests[f] = slices.Contains(sel, i+1)
		}
		return true, nil
	}
}

const depsNotice = `Dependency detection reads the package manager files you select (package.json, go.mod,
pom.xml, build.gradle, ...) in each repository, only to detect dependencies.
Only dependency names and versions are kept; file contents and paths are never stored or sent.`

func cmdDeps(dir string, args []string) error {
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	repos, err := loadRepos(dir)
	if err != nil {
		return err
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "":
		fmt.Println("dependency detection:", map[bool]string{true: "on", false: "off"}[cfg.Deps])
		for _, r := range repos {
			allowed := 0
			for _, ok := range r.Manifests {
				if ok {
					allowed++
				}
			}
			fmt.Printf("  %s: %d allowed, %d waiting for review\n", r.Name, allowed, pendingManifests(r))
		}
		return nil
	case "on":
		fmt.Println(depsNotice)
		cfg.Deps = true
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		if !interactive() {
			fmt.Println("Run `gitfolio deps review` in each repository to choose files.")
			return nil
		}
		for i := range repos {
			if err := reviewAndRescan(dir, &repos[i]); err != nil {
				return err
			}
		}
		return saveRepos(dir, repos)
	case "off":
		cfg.Deps = false
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(dir, "deps.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for i := range repos {
			repos[i].Manifests = nil
		}
		commits, err := readCommits(dir)
		if err != nil {
			return err
		}
		for i := range commits {
			for j := range commits[i].Files {
				commits[i].Files[j].Module = ""
			}
		}
		if err := writeCommits(dir, commits); err != nil {
			return err
		}
		fmt.Println("dependency detection is off; collected dependencies were deleted")
		return saveRepos(dir, repos)
	case "review":
		if !cfg.Deps {
			return errors.New("dependency detection is off (turn it on with: gitfolio deps on)")
		}
		top, err := topLevel(pathArg(args[1:]))
		if err != nil {
			return err
		}
		i := slices.IndexFunc(repos, func(r Repo) bool { return r.Path == top })
		if i < 0 {
			return fmt.Errorf("%s is not registered (run: gitfolio add)", top)
		}
		if err := reviewAndRescan(dir, &repos[i]); err != nil {
			return err
		}
		return saveRepos(dir, repos)
	}
	return errors.New("usage: gitfolio deps [on|off|review [path]]")
}

// reviewAndRescan asks about r's manifest files and, when decisions changed, collects r again so
// dependencies and module IDs match the new approvals.
func reviewAndRescan(dir string, r *Repo) error {
	changed, err := reviewManifests(r)
	if err != nil || !changed {
		return err
	}
	n, err := scanRepo(dir, r, true)
	if err != nil {
		return err
	}
	fmt.Printf("%s: dependencies updated, %d commits collected again\n", r.Name, n)
	return nil
}
