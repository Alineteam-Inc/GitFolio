package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIBase(t *testing.T) {
	for url, ok := range map[string]bool{
		"":                           true, // default
		"https://staging.example.com": true,
		"http://127.0.0.1:8080":      true,
		"http://localhost:8080":      true,
		"http://aline.team":          false,
		"ftp://aline.team":           false,
		"aline.team":                 false,
	} {
		t.Setenv("GITFOLIO_API_URL", url)
		if _, err := apiBase(""); (err == nil) != ok {
			t.Errorf("apiBase with env %q: err = %v, want ok=%v", url, err, ok)
		}
	}
	// The environment wins over the saved setting, which wins over production.
	t.Setenv("GITFOLIO_API_URL", "")
	if got, _ := apiBase("https://staging.example.com/api/"); got != "https://staging.example.com/api" {
		t.Errorf("configured base = %q", got)
	}
	if got, _ := apiBase(""); got != defaultAPIBase {
		t.Errorf("default base = %q", got)
	}
	t.Setenv("GITFOLIO_API_URL", "http://127.0.0.1:9")
	if got, _ := apiBase("https://staging.example.com/api"); got != "http://127.0.0.1:9" {
		t.Errorf("env should override config, got %q", got)
	}
}

// fakeAline follows the aline.team contract in docs/API.md (ApiBody envelope, ErrorCode codes)
// closely enough to check what the CLI sends and how it handles the answers.
type fakeAline struct {
	token    string
	exists   bool  // the email already has an account
	verified bool  // verify was called
	notified *bool // isNotified as sent in verify; nil when left out
}

const testToken = "aln_cli_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"

// testAccountID has the real server's shape: a UUID string, not a number (seen on [dev server]).
const testAccountID = "d3bbee5d-84fc-422b-97fc-a021db3f7f66"

func (f *fakeAline) handler() http.Handler {
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, contents any) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"success": true}
		if contents != nil {
			body["contents"] = contents
		}
		json.NewEncoder(w).Encode(body)
	}
	fail := func(w http.ResponseWriter, status int, code string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{
			"status": status, "code": code, "messageKo": "오류 " + code, "messageEn": "error " + code, "messageJa": "エラー " + code,
		}})
	}
	authed := func(r *http.Request) bool { return f.token != "" && r.Header.Get("Authorization") == "Bearer "+f.token }

	mux.HandleFunc("POST /gitfolio/auth/email/start", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Email string }
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Email == "" {
			fail(w, 400, "C001")
			return
		}
		ok(w, map[string]any{"challengeId": "ch_1", "expiresAt": time.Now().UTC().Add(10 * time.Minute), "codeLength": 6, "accountExists": f.exists})
	})
	mux.HandleFunc("POST /gitfolio/auth/email/verify", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ChallengeID string `json:"challengeId"`
			Code        string `json:"code"`
			Device      struct{ Name, Language, Timezone string }
			IsNotified  *bool `json:"isNotified"`
		}
		f.verified = true
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.ChallengeID != "ch_1" || in.Device.Name == "" ||
			(in.Device.Language != "" && in.Device.Language != "KO" && in.Device.Language != "EN" && in.Device.Language != "JA") {
			fail(w, 400, "C001") // the server takes upper-case KO/EN/JA only
			return
		}
		f.notified = in.IsNotified
		if in.Code != "123456" {
			fail(w, 400, "A008")
			return
		}
		f.token = testToken
		ok(w, map[string]any{
			"token": f.token, "tokenExpiresAt": time.Now().UTC().Add(90 * 24 * time.Hour),
			"account":        map[string]any{"id": testAccountID, "email": "dev@example.com", "created": !f.exists},
			"verifiedEmails": []string{"dev@example.com"},
		})
	})
	mux.HandleFunc("GET /gitfolio/me", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			fail(w, 401, "A001")
			return
		}
		ok(w, map[string]any{"account": map[string]any{"id": testAccountID, "email": "dev@example.com"}, "verifiedEmails": []string{"dev@example.com"}})
	})
	mux.HandleFunc("POST /gitfolio/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			fail(w, 401, "A001")
			return
		}
		f.token = ""
		ok(w, nil) // 200 {"success": true}
	})
	return mux
}

func TestSignInMeLogout(t *testing.T) {
	f := &fakeAline{}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	dir := t.TempDir()

	c, err := newClient(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITFOLIO_LANG", "ko")       // sent as device.language "KO"
	codes := []string{"000000", "123456"} // a wrong code (A008) first, then the right one
	asked := false
	res, err := c.signIn("dev@example.com", func() (bool, bool) { asked = true; return true, true }, func(retry bool) string {
		code := codes[0]
		codes = codes[1:]
		return code
	})
	if err != nil || !res.Account.Created || !asked {
		t.Fatalf("signIn = %+v, %v (sign-up confirmed: %v)", res, err, asked)
	}
	if f.notified == nil || !*f.notified {
		t.Errorf("isNotified = %v, want true for a sign-up that opted in", f.notified)
	}
	if saved, _ := loadCredentials(dir); saved.Token != testToken || saved.Email != "dev@example.com" {
		t.Fatalf("credentials after sign-in = %+v", saved)
	}

	var me struct {
		Account struct{ Email string } `json:"account"`
	}
	if err := c.call("GET", "/gitfolio/me", nil, &me); err != nil || me.Account.Email != "dev@example.com" {
		t.Fatalf("GET /gitfolio/me = %+v, %v", me, err)
	}

	if err := cmdWhoami(dir); err != nil {
		t.Fatalf("whoami: %v", err)
	}

	if err := cmdLogout(dir); err != nil {
		t.Fatal(err)
	}
	if f.token != "" {
		t.Error("logout did not revoke the token on the server")
	}
	if saved, _ := loadCredentials(dir); saved != (Credentials{}) {
		t.Errorf("credentials after logout = %+v, want empty", saved)
	}
}

// An existing account signs in without the sign-up notice; declining the notice for a new email
// sends no code and makes no account.
func TestSignupConfirmation(t *testing.T) {
	for _, tc := range []struct {
		exists, confirm, wantAsked, wantVerify bool
	}{
		{exists: true, confirm: false, wantAsked: false, wantVerify: true},
		{exists: false, confirm: false, wantAsked: true, wantVerify: false},
	} {
		f := &fakeAline{exists: tc.exists}
		srv := httptest.NewServer(f.handler())
		t.Setenv("GITFOLIO_API_URL", srv.URL)
		c, err := newClient(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		asked := false
		_, err = c.signIn("dev@example.com", func() (bool, bool) { asked = true; return tc.confirm, true }, func(bool) string { return "123456" })
		srv.Close()
		if asked != tc.wantAsked || f.verified != tc.wantVerify {
			t.Errorf("exists=%v: notice shown %v, verify called %v (err %v)", tc.exists, asked, f.verified, err)
		}
		if tc.exists && f.notified != nil {
			t.Errorf("isNotified sent for an existing account: %v", *f.notified)
		}
		if !tc.wantVerify && err != errSignupCancelled {
			t.Errorf("declined sign-up returned %v, want errSignupCancelled", err)
		}
	}
}

func TestZoneFromPath(t *testing.T) {
	for in, want := range map[string]string{
		"/var/db/timezone/zoneinfo/Asia/Seoul":   "Asia/Seoul",
		"../usr/share/zoneinfo/America/New_York": "America/New_York",
		"/usr/share/zoneinfo/UTC":                "UTC",
		"/etc/something-else":                    "",
	} {
		if got := zoneFromPath(in); got != want {
			t.Errorf("zoneFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// A token the server rejects (A001: expired, revoked or unknown) is removed locally.
func TestRejectedTokenIsRemoved(t *testing.T) {
	f := &fakeAline{} // no valid token on the server
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	t.Setenv("GITFOLIO_API_URL", srv.URL)
	dir := t.TempDir()
	if err := saveCredentials(dir, Credentials{Token: testToken, Email: "dev@example.com"}); err != nil {
		t.Fatal(err)
	}
	c, err := newClient(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = c.call("GET", "/gitfolio/me", nil, nil)
	if ae, ok := err.(interface{ Unwrap() error }); err == nil || !ok || ae.Unwrap() == nil {
		t.Fatalf("call with a rejected token = %v, want an A001 error", err)
	}
	if saved, _ := loadCredentials(dir); saved.Token != "" {
		t.Errorf("rejected token kept: %+v", saved)
	}
}
