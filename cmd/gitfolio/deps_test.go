package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	for _, tc := range []struct{ file, content, want string }{
		{"package.json", `{"dependencies":{"react":"^18.2.0","local":"file:../x","gh":"user/repo","ws":"workspace:*"},
			"devDependencies":{"typescript":"5.4.0","react":"^18.2.0"}}`, "react@^18.2.0 typescript@5.4.0"},
		{"go.mod", "module x\n\ngo 1.22\n\nrequire github.com/a/b v1.0.0\n\nrequire (\n\tgithub.com/c/d v2.1.0\n\tgithub.com/e/f v0.1.0 // indirect\n)\n",
			"github.com/a/b@v1.0.0 github.com/c/d@v2.1.0"},
		{"requirements.txt", "# pinned\nDjango==4.2\nrequests[socks]>=2.31 ; python_version > '3.8'\n-r base.txt\n./local\ngit+https://x/y.git\nnumpy\n",
			"Django@4.2 numpy@ requests@>=2.31"},
		{"pyproject.toml", "[project]\nname = \"x\"\ndependencies = [\n  \"fastapi>=0.110\",\n  \"uvicorn[standard]\",\n]\n\n[tool.poetry.dependencies]\npython = \"^3.11\"\npydantic = \"^2.0\"\nmylib = { path = \"../mylib\" }\n",
			"fastapi@>=0.110 pydantic@^2.0 uvicorn@"},
		{"pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0"><dependencies>
			<dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency>
			<dependency><groupId>junit</groupId><artifactId>junit</artifactId><version>4.13.2</version></dependency>
			</dependencies></project>`, "junit:junit@4.13.2 org.springframework.boot:spring-boot-starter-web@"},
		{"build.gradle.kts", "dependencies {\n  implementation(\"io.ktor:ktor-server-core:2.3.0\")\n  testImplementation(platform(\"org.junit:junit-bom:5.10.0\"))\n  implementation(project(\":core\"))\n  implementation(libs.okhttp)\n}\n",
			"io.ktor:ktor-server-core@2.3.0 org.junit:junit-bom@5.10.0"},
		{"libs.versions.toml", "[versions]\nokhttp = \"4.12.0\"\n\n[libraries]\nokhttp = { module = \"com.squareup.okhttp3:okhttp\", version.ref = \"okhttp\" }\ngson = \"com.google.code.gson:gson:2.10\"\ncoil = { group = \"io.coil-kt\", name = \"coil\", version = \"2.5.0\" }\n",
			"com.google.code.gson:gson@2.10 com.squareup.okhttp3:okhttp@4.12.0 io.coil-kt:coil@2.5.0"},
		{"Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n\n[dependencies]\nserde = { version = \"1.0\", features = [\"derive\"] }\ntokio = \"1\"\nlocal = { path = \"../local\" }\n\n[dependencies.reqwest]\nversion = \"0.12\"\n\n[dev-dependencies]\ninsta = \"1.34\"\n",
			"insta@1.34 reqwest@0.12 serde@1.0 tokio@1"},
	} {
		var got []string
		for _, d := range parseManifest(tc.file, tc.content) {
			got = append(got, d.name+"@"+d.version)
		}
		if s := strings.Join(got, " "); s != tc.want {
			t.Errorf("%s:\n got  %s\n want %s", tc.file, s, tc.want)
		}
	}
}

func TestParseSelection(t *testing.T) {
	for in, want := range map[string]string{"all": "[1 2 3 4]", "none": "[]", "1,3": "[1 3]", "2-4, 1": "[1 2 3 4]", "3,3": "[3]"} {
		sel, err := parseSelection(in, 4)
		if got := fmt.Sprint(sel); err != nil || got != want {
			t.Errorf("parseSelection(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "5", "3-1", "x", "1-"} {
		if _, err := parseSelection(bad, 4); err == nil {
			t.Errorf("parseSelection(%q) accepted", bad)
		}
	}
}

// TestDepsInMonorepo approves two of three manifest files and checks that only those are read,
// and that each changed file points at the module of its nearest approved manifest.
func TestDepsInMonorepo(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, data := t.TempDir(), t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	files := map[string]string{
		"package.json":           `{"dependencies":{"react":"18.0.0"}}`,
		"services/api/go.mod":    "module api\n\nrequire github.com/gin-gonic/gin v1.9.0\n",
		"services/api/main.go":   "package main\n",
		"tools/requirements.txt": "secret-internal-tool==1.0\n",
		"web/src/App.jsx":        "export default 1\n",
		"node_modules/x/go.mod":  "module x\n",
	}
	for name, body := range files {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "me@example.com")
	run("config", "user.name", "me")
	run("add", "-f", ".")
	run("commit", "-q", "-m", "init")

	r := newRepo(repo)
	if got := strings.Join(manifestCandidates(repo), " "); got != "package.json services/api/go.mod tools/requirements.txt" {
		t.Fatalf("candidates = %q (node_modules must be skipped)", got)
	}
	r.Manifests = map[string]bool{"package.json": true, "services/api/go.mod": true, "tools/requirements.txt": false}
	if err := saveConfig(data, Config{Deps: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := scanRepo(data, &r, false); err != nil {
		t.Fatal(err)
	}

	deps, err := loadDeps(data)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range deps {
		names = append(names, d.Ecosystem+":"+d.Name)
	}
	if got := strings.Join(names, " "); got != "npm:react go:github.com/gin-gonic/gin" && got != "go:github.com/gin-gonic/gin npm:react" {
		t.Errorf("deps = %q; the declined requirements.txt must not be read", got)
	}

	commits, err := readCommits(data)
	if err != nil || len(commits) != 1 {
		t.Fatalf("commits = %v, %v", commits, err)
	}
	module := map[string]string{}
	for _, f := range commits[0].Files {
		module[f.Name] = f.Module
	}
	if module["main.go"] != moduleID("services/api") || module["App.jsx"] != moduleID("") || module["requirements.txt"] != moduleID("") {
		t.Errorf("modules = %v; want main.go in services/api, the rest in the root module", module)
	}
}

// TestExportOnlyTouchedModules checks that export sends the dependencies of modules the user's commits
// touched, and never the module IDs themselves.
func TestExportOnlyTouchedModules(t *testing.T) {
	data := t.TempDir()
	if err := saveRepos(data, []Repo{{ID: "r1", Name: "mono", Provider: "GITHUB", Namespace: "o/mono"}}); err != nil {
		t.Fatal(err)
	}
	if err := appendCommits(data, []Commit{{Repo: "r1", Hash: "h1", Files: []FileStat{{Name: "main.go", Add: 1, Module: "api"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := saveJSON(filepath.Join(data, "deps.json"), []Dependency{
		{Repo: "r1", Module: "api", Ecosystem: "go", Name: "github.com/gin-gonic/gin"},
		{Repo: "r1", Module: "web", Ecosystem: "npm", Name: "react"},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := buildExport(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Dependencies) != 1 || out.Dependencies[0].Name != "github.com/gin-gonic/gin" || out.Dependencies[0].Namespace != "o/mono" {
		t.Errorf("dependencies = %+v; want only gin from the touched api module", out.Dependencies)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), `"module"`) {
		t.Errorf("export contains module IDs: %s", b)
	}
}
