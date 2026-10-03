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
	return c.login(lang)
}

// login asks for an email and signs in with a code sent to it; an email without an account is signed
// up after showing the terms, the privacy policy and the email it will be made for.
func (c *client) login(lang string) error {
	section(lang, "loginTitle")
	email := ""
	for {
		email = strings.TrimSpace(prompt(tr(lang, "emailAsk")))
		if stdinClosed {
			return failure("emailInvalid")
		}
		// Catch typos (e.g. a leftover IME character and a space) here, before the server
		// rejects the request and the whole login has to start over.
		if validEmail(email) {
			break
		}
		notice(tr(lang, "emailInvalid"))
	}
	confirmSignup := func() (ok, notify bool) {
		notice("\n" + fmt.Sprintf(tr(lang, "signupNotice"), email))
		a := strings.ToLower(prompt(tr(lang, "signupAsk")))
		if stdinClosed || (a != "" && a != "y" && a != "yes") {
			return false, false
		}
		n := strings.ToLower(prompt(tr(lang, "notifyAsk"))) // opt-in: only an explicit yes
		return true, n == "y" || n == "yes"
	}
	res, err := c.signIn(email, confirmSignup, askCode(lang, email, false))
	if errors.Is(err, errSignupCancelled) {
		say(lang, "signupCancelled")
		return nil
	}
	if err != nil {
		return err
	}
	if res.Account.Created {
		show(out, fmt.Sprintf(tr(lang, "signedUp"), res.Account.Email)+tr(lang, "passwordMail"))
	} else {
		say(lang, "loggedIn", res.Account.Email)
	}
	return nil
}

// askCode asks for the code sent to email, again while it is not the announced number of digits.
// When skippable, Enter alone returns "" (a work email can be verified later).
func askCode(lang, email string, skippable bool) func(retry bool, length int) string {
	ask := map[bool]string{false: "codeAsk", true: "codeAskSkip"}[skippable]
	return func(retry bool, length int) string {
		if retry {
			notice(tr(lang, "codeWrong"))
		}
		for {
			code := strings.TrimSpace(prompt(fmt.Sprintf(tr(lang, ask), email)))
			// A typo is caught here, so it neither ends the login nor uses up one of the server's tries.
			if stdinClosed || validCode(code, length) || (skippable && code == "") {
				return code
			}
			notice(fmt.Sprintf(tr(lang, "codeFormat"), length))
		}
	}
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
	if !c.ensureLogin() {
		say(lang, "notLoggedIn")
		return nil
	}
	me, err := c.me()
	if err != nil {
		return err
	}
	sayKV(lang, "whoami", me.Account.Email, strings.Join(me.VerifiedEmails, ", "), c.base)
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
