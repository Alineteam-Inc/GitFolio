package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// cmdLogin signs in to aline.team with an email code; an email without an account is signed up
// (DESIGN 6.1). Signing up counts as agreeing to the terms and the privacy policy, so a new account
// is only made after showing both and the email it will be made for.
func cmdLogin(dir string) error {
	lang := detectLang(os.Getenv)
	c, err := newClient(dir)
	if err != nil {
		return err
	}
	if c.loggedIn() {
		say(lang, "alreadyLoggedIn", c.creds.Email)
		return nil
	}
	if !interactive() {
		return failure("loginNeedsTerminal")
	}
	section(lang, "loginTitle")
	email := ""
	for {
		email = strings.TrimSpace(prompt(tr(lang, "emailAsk")))
		if stdinClosed {
			return failure("emailInvalid")
		}
		// Catch typos (e.g. a leftover IME character and a space) here, before the server
		// rejects the request and the whole login has to start over.
		if strings.Count(email, "@") == 1 && !strings.ContainsAny(email, " \t") {
			break
		}
		fmt.Print(indent(tr(lang, "emailInvalid")))
	}
	confirmSignup := func() (ok, notify bool) {
		fmt.Print(indent("\n" + fmt.Sprintf(tr(lang, "signupNotice"), email)))
		a := strings.ToLower(prompt(tr(lang, "signupAsk")))
		if stdinClosed || (a != "" && a != "y" && a != "yes") {
			return false, false
		}
		n := strings.ToLower(prompt(tr(lang, "notifyAsk"))) // opt-in: only an explicit yes
		return true, n == "y" || n == "yes"
	}
	askCode := func(retry bool, length int) string {
		if retry {
			fmt.Print(indent(tr(lang, "codeWrong")))
		}
		for {
			code := strings.TrimSpace(prompt(fmt.Sprintf(tr(lang, "codeAsk"), email)))
			// A typo is caught here, so it neither ends the login nor uses up one of the server's tries.
			if stdinClosed || validCode(code, length) {
				return code
			}
			fmt.Print(indent(fmt.Sprintf(tr(lang, "codeFormat"), length)))
		}
	}
	res, err := c.signIn(email, confirmSignup, askCode)
	if errors.Is(err, errSignupCancelled) {
		say(lang, "signupCancelled")
		return nil
	}
	if err != nil {
		return err
	}
	if res.Account.Created {
		show(os.Stdout, fmt.Sprintf(tr(lang, "signedUp"), res.Account.Email)+tr(lang, "passwordMail"))
	} else {
		say(lang, "loggedIn", res.Account.Email)
	}
	return nil
}

// validCode reports whether code is exactly length digits.
func validCode(code string, length int) bool {
	return len(code) == length && strings.Trim(code, "0123456789") == ""
}

// cmdWhoami asks aline.team which account this device's token belongs to (GET /cli/me).
func cmdWhoami(dir string) error {
	lang := detectLang(os.Getenv)
	c, err := newClient(dir)
	if err != nil {
		return err
	}
	if !c.loggedIn() {
		say(lang, "notLoggedIn")
		return nil
	}
	var me struct {
		Account struct {
			Email string `json:"email"`
		} `json:"account"`
		VerifiedEmails []string `json:"verifiedEmails"`
	}
	if err := c.call("GET", "/cli/me", nil, &me); err != nil {
		return err
	}
	say(lang, "whoami", me.Account.Email, strings.Join(me.VerifiedEmails, ", "), c.base)
	return nil
}

// cmdLogout revokes this device's token on aline.team (best effort) and deletes it here.
func cmdLogout(dir string) error {
	c, err := newClient(dir)
	if err != nil {
		return err
	}
	if c.loggedIn() {
		if err := c.call("POST", "/cli/logout", nil, nil); err != nil {
			warn(detectLang(os.Getenv), "logoutServerFailed", err)
		}
	}
	if err := saveCredentials(dir, Credentials{}); err != nil {
		return err
	}
	say(detectLang(os.Getenv), "loggedOut")
	return nil
}
