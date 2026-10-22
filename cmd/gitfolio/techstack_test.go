package main

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"testing"
)

// The techs dependency names show here are exactly the ones aline.team finds for the same names: every
// case aline.team's own resolver gave (testdata/stack-golden.json, made from its rules and their edge
// cases) gives the same techs, names, categories and lists here.
func TestStackGolden(t *testing.T) {
	b, err := os.ReadFile("testdata/stack-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Ecosystem, Kind string
			Input           []string
			Expected        []struct{ Key, Name, Category, WebClass string }
		}
	}
	if err := json.Unmarshal(b, &golden); err != nil || len(golden.Cases) < 300 {
		t.Fatalf("golden data: %d cases, %v", len(golden.Cases), err)
	}
	for _, c := range golden.Cases {
		techs := depTechs(c.Ecosystem, c.Input)
		if c.Ecosystem == "file-presence" { // paths in a repository, the techs of all of them
			for _, p := range c.Input {
				for _, k := range filePresenceTechs(p) {
					techs[k] = true
				}
			}
		}
		got := slices.Sorted(maps.Keys(techs))
		var want []string
		for _, e := range c.Expected {
			want = append(want, e.Key)
			if info := techCatalog[e.Key]; info.name != e.Name || info.category != e.Category || techClass(info.category) != e.WebClass {
				t.Errorf("%s: catalog %+v, want %s %s %s", e.Key, info, e.Name, e.Category, e.WebClass)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s %s %q: %v, want %v", c.Ecosystem, c.Kind, c.Input, got, want)
		}
	}
}
