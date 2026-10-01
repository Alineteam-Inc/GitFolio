package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// The user's work emails (DESIGN 3.2): commits by any of them, or by a repository's git config
// user.email, count as the user's own. aline.team gets them all under one author email, the primary: the
// first in the list. aline.team trusts these emails, so there is no verification step.

// primaryEmail is the author email sent for all of the user's commits: the first work email, else the
// global git config user.email.
func primaryEmail(cfg Config) string {
	if len(cfg.Emails) > 0 {
		return cfg.Emails[0]
	}
	out, _ := git(".", "config", "--global", "user.email")
	return strings.ToLower(strings.TrimSpace(out))
}

func validEmail(e string) bool {
	return strings.Count(e, "@") == 1 && !strings.HasPrefix(e, "@") && !strings.HasSuffix(e, "@") && !strings.ContainsAny(e, " \t")
}

// addEmails adds the valid, new emails to the list (lowercased) and returns the ones it could not take.
func addEmails(cfg *Config, emails []string) (bad []string) {
	for _, e := range emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e == "" {
			continue
		}
		if !validEmail(e) {
			bad = append(bad, e)
		} else if !slices.Contains(cfg.Emails, e) {
			cfg.Emails = append(cfg.Emails, e)
		}
	}
	return bad
}

// emailList shows the work emails, the primary marked with ★.
func emailList(lang string, cfg Config) string {
	if len(cfg.Emails) == 0 {
		return tr(lang, "emailNone")
	}
	s := tr(lang, "emailList")
	for i, e := range cfg.Emails {
		s += map[bool]string{true: "★ ", false: "  "}[i == 0] + e + "\n"
	}
	return s
}

// cmdEmail shows or changes the work emails: add, rm, primary.
func cmdEmail(dir string, args []string) error {
	lang := detectLang(os.Getenv)
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		show(os.Stdout, emailList(lang, cfg))
		return nil
	}
	if len(args) < 2 || (args[0] != "add" && args[0] != "rm" && args[0] != "primary") {
		return failure("usage", "gitfolio email [add <email>... | rm <email> | primary <email>]")
	}
	e := strings.ToLower(strings.TrimSpace(args[1]))
	var done string
	switch args[0] {
	case "add":
		if bad := addEmails(&cfg, args[1:]); len(bad) > 0 {
			return failure("emailBad", strings.Join(bad, ", "))
		}
		done = tr(lang, "emailAdded")
	case "rm":
		i := slices.Index(cfg.Emails, e)
		switch {
		case i < 0:
			return failure("emailNotFound", e)
		case i == 0:
			return failure("emailRmPrimary", e) // pick another primary first, so there always is one
		}
		cfg.Emails = slices.Delete(cfg.Emails, i, i+1)
		done = tr(lang, "emailRemoved")
	case "primary":
		if !validEmail(e) {
			return failure("emailBad", e)
		}
		cfg.Emails = append([]string{e}, slices.DeleteFunc(cfg.Emails, func(x string) bool { return x == e })...)
		done = fmt.Sprintf(tr(lang, "emailPrimary"), e)
	}
	if err := saveConfig(dir, cfg); err != nil {
		return err
	}
	show(os.Stdout, done+emailList(lang, cfg))
	return nil
}
