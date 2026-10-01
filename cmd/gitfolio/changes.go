package main

import (
	"regexp"
	"strings"
	"unicode"
)

// firstChanges finds, for each file the user created, the author date of the first later commit by
// someone else that changed it: aline.team's modifiedFiles. Files are followed by path, the
// way aline.team's own git sync does: a file renamed away is not followed, and someone else's rename is
// not a change to the created path. System commits (merge, release, ...) do not count. commits is in
// git log order, newest first; the result maps path → author date.
func firstChanges(commits []Commit, mine map[string]bool) map[string]string {
	created, first := map[string]bool{}, map[string]string{}
	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]
		if creationType(c, mine) != "" {
			for _, f := range c.Files {
				if f.Created {
					created[f.Name] = true
				}
			}
			continue
		}
		if systemCommit(c.Message) {
			continue
		}
		for _, f := range c.Files {
			if created[f.Name] && first[f.Name] == "" {
				first[f.Name] = c.Date
			}
		}
	}
	if len(first) == 0 {
		return nil
	}
	return first
}

var (
	conventionalType = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert|merge)(\([^)]*\))?!?:`)
	systemFirstWords = map[string]bool{"merge": true, "merged": true, "rebase": true, "rollback": true, "squash": true,
		"release": true, "cherry-pick": true, "cherrypick": true}
	systemShortWords = map[string]bool{"merged": true, "squashed": true, "initial": true, "automated": true, "auto": true, "bot": true}
)

// systemCommit reports whether aline.team counts the commit as a system one, going by its subject:
// a Conventional Commits "merge:" type (other types, "chore(release):" and "revert:" included, are
// changes; "release:" is no type, so the next rule applies); else a first word like merge, release or rebase; else a short subject with a word like
// initial or automated ("Initial commit"). Merge commits are left out of git log already.
// ponytail: the server's third rule weighs a whole keyword table; only its short-subject case is here.
func systemCommit(message string) bool {
	subject := strings.ToLower(strings.TrimSpace(strings.SplitN(strings.TrimSpace(message), "\n", 2)[0]))
	if m := conventionalType.FindStringSubmatch(subject); m != nil {
		return m[1] == "merge"
	}
	words := strings.FieldsFunc(subject, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' })
	if len(words) == 0 {
		return false
	}
	if systemFirstWords[words[0]] {
		return true
	}
	if len(words) <= 2 {
		for _, w := range words {
			if systemShortWords[w] {
				return true
			}
		}
	}
	return false
}
