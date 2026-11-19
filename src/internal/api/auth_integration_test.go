package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestAuthRoutesLoginMeRotationLogoutIntegration(t *testing.T) {
	f := newFixture(t)
	f.store.account.Account.PlatformAdmin = true
	a := newAuthAPI(t, f, &fakeLeadership{groups: []security.ID{testGroup}})
	w := dispatch(a, loginRequest())
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	cookieHeader := w.Result().Cookies()[0]
	cookie := security.NewSecret(cookieHeader.Value)
	loginBody := w.Body.String()
	view := readSession(t, w)
	if view.User.ID != testUser || view.User.Username != "viewer" || !view.User.Active || !view.User.PlatformAdmin || !view.PlatformAdmin || len(view.CSRFToken) != 43 || len(view.LeaderOf) != 1 {
		t.Fatal("login response differs from K1 AuthMe")
	}
	if !cookieHeader.HttpOnly || !cookieHeader.Secure || cookieHeader.SameSite != http.SameSiteLaxMode || cookieHeader.Path != "/" || w.Header().Get("Location") != "" {
		t.Fatal("unsafe login cookie or HTML redirect")
	}
	if strings.Contains(loginBody, cookie.Reveal()) || strings.Contains(loginBody, "correct test password") {
		t.Fatal("login exposed credentials/session")
	}
	var sessionID security.ID
	for id := range f.store.sessions {
		sessionID = id
	}
	if strings.Contains(loginBody, string(sessionID)) {
		t.Fatal("session family ID appeared in JSON")
	}
	f.clock.now = f.clock.now.Add(security.DefaultSessionPolicy().RotationInterval)
	w = dispatch(a, request("GET", Prefix+"/auth/me", "", cookie, security.Secret{}))
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Value == cookie.Reveal() {
		t.Fatal("me did not rotate cookie")
	}
	rotated := security.NewSecret(w.Result().Cookies()[0].Value)
	rotatedView := readSession(t, w)
	if rotatedView.CSRFToken != view.CSRFToken {
		t.Fatal("routine rotation unexpectedly changed CSRF epoch")
	}
	w = dispatch(a, request("POST", Prefix+"/auth/logout", "", rotated, security.NewSecret(rotatedView.CSRFToken)))
	if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout failed: %d %s", w.Code, w.Body.String())
	}
	for _, token := range []security.Secret{cookie, rotated} {
		w = dispatch(a, request("GET", Prefix+"/auth/me", "", token, security.Secret{}))
		if w.Code != 401 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
			t.Fatal("revoked session/grace token accepted")
		}
	}
	for _, secret := range []string{cookie.Reveal(), rotated.Reveal(), view.CSRFToken, string(sessionID), "correct test password"} {
		if strings.Contains(f.logs.String(), secret) {
			t.Fatal("auth logs leaked a secret")
		}
	}
	if !strings.Contains(f.logs.String(), `"event":"auth.login"`) || !strings.Contains(f.logs.String(), `"event":"auth.logout"`) {
		t.Fatal("auth audit events missing")
	}
}

func TestAuthMeRefreshesCSRFEpochAndRejectsStaleLogout(t *testing.T) {
	f := newFixture(t)
	a := newAuthAPI(t, f, &fakeLeadership{})
	w := dispatch(a, loginRequest())
	cookie := security.NewSecret(w.Result().Cookies()[0].Value)
	view := readSession(t, w)
	f.store.account.Account.AuthVersion++
	w = dispatch(a, request("GET", Prefix+"/auth/me", "", cookie, security.Secret{}))
	if w.Code != 200 {
		t.Fatal("me failed to refresh auth version")
	}
	newCookie := security.NewSecret(w.Result().Cookies()[0].Value)
	fresh := readSession(t, w)
	if fresh.CSRFToken == view.CSRFToken {
		t.Fatal("CSRF epoch was not refreshed")
	}
	w = dispatch(a, request("POST", Prefix+"/auth/logout", "", newCookie, security.NewSecret(view.CSRFToken)))
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"code":"csrf_failed"`) {
		t.Fatal("stale token revoked elevated session")
	}
	w = dispatch(a, request("GET", Prefix+"/auth/me", "", newCookie, security.Secret{}))
	if w.Code != 200 {
		t.Fatal("invalid logout revoked session")
	}
	w = dispatch(a, request("POST", Prefix+"/auth/logout", "", newCookie, security.NewSecret(fresh.CSRFToken)))
	if w.Code != 204 {
		t.Fatal("fresh token could not logout")
	}
}

func TestAuthLoginReplacesPreviousSessionWithoutFixation(t *testing.T) {
	f := newFixture(t)
	a := newAuthAPI(t, f, &fakeLeadership{})
	w := dispatch(a, loginRequest())
	old := security.NewSecret(w.Result().Cookies()[0].Value)
	r := loginRequest()
	r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: old.Reveal()})
	w = dispatch(a, r)
	if w.Code != 200 || w.Result().Cookies()[0].Value == old.Reveal() {
		t.Fatal("login reused session token")
	}
	w = dispatch(a, request("GET", Prefix+"/auth/me", "", old, security.Secret{}))
	if w.Code != 401 {
		t.Fatal("replaced session remains usable")
	}
}

func TestAuthDirectoryFailurePreservesCommittedSessionForRecovery(t *testing.T) {
	f := newFixture(t)
	leaders := &fakeLeadership{err: errors.New("private-source-canary")}
	a := newAuthAPI(t, f, leaders)
	w := dispatch(a, loginRequest())
	if w.Code != 503 || len(w.Result().Cookies()) != 1 || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("directory outage fabricated success or stranded committed session")
	}
	cookie := security.NewSecret(w.Result().Cookies()[0].Value)
	leaders.err = nil
	leaders.groups = []security.ID{testGroup}
	w = dispatch(a, request("GET", Prefix+"/auth/me", "", cookie, security.Secret{}))
	view := readSession(t, w)
	if len(view.LeaderOf) != 1 {
		t.Fatal("profile could not recover without another login")
	}
	// Protected must likewise preserve its rotated cookie when a later read fails.
	f.clock.now = f.clock.now.Add(security.DefaultSessionPolicy().RotationInterval)
	leaders.err = errors.New("private-source-canary")
	w = dispatch(a, request("GET", Prefix+"/auth/me", "", cookie, security.Secret{}))
	if w.Code != 503 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Value == cookie.Reveal() {
		t.Fatal("profile failure lost rotated cookie")
	}
}

func TestAuthRoutesRejectInvalidSecurityTransportAndExpiredSessions(t *testing.T) {
	for _, scenario := range []string{"login origin", "login media", "login query", "me body", "me query", "logout body", "logout query", "logout CSRF query", "logout no csrf", "logout foreign origin", "logout anonymous", "expired", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			leaders := &fakeLeadership{}
			a := newAuthAPI(t, f, leaders)
			session := fixtureLogin(t, f)
			r := request("POST", Prefix+"/auth/logout", "", session.CookieToken, session.CSRFToken)
			want := 403
			switch scenario {
			case "login origin":
				r = loginRequest()
				r.Header.Set("Origin", "https://attacker.example")
			case "login media":
				r = loginRequest()
				r.Header.Set("Content-Type", "text/plain")
				want = 415
			case "login query":
				r = loginRequest()
				r.URL.RawQuery = "extra=1"
				want = 400
			case "me body":
				r = request("GET", Prefix+"/auth/me", `{}`, session.CookieToken, security.Secret{})
				want = 400
			case "me query":
				r = request("GET", Prefix+"/auth/me?extra=1", "", session.CookieToken, security.Secret{})
				want = 400
			case "logout body":
				r = request("POST", Prefix+"/auth/logout", `{}`, session.CookieToken, session.CSRFToken)
				want = 400
			case "logout query":
				r.URL.RawQuery = "extra=1"
				want = 400
			case "logout CSRF query":
				r.URL.RawQuery = "csrf_token=" + session.CSRFToken.Reveal()
			case "logout no csrf":
				r.Header.Del(security.CSRFHeaderName)
			case "logout foreign origin":
				r.Header.Set("Origin", "https://attacker.example")
			case "logout anonymous":
				r.Header.Del("Cookie")
				want = 401
			case "expired":
				r = request("GET", Prefix+"/auth/me", "", session.CookieToken, security.Secret{})
				f.clock.now = f.clock.now.Add(30 * time.Minute)
				want = 401
			case "disabled":
				r = request("GET", Prefix+"/auth/me", "", session.CookieToken, security.Secret{})
				f.store.account.Account.Active = false
				want = 401
			}
			w := dispatch(a, r)
			var body ErrorEnvelope
			if w.Code != want || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.RequestID != w.Header().Get("X-Request-ID") {
				t.Fatalf("%s: %d %s", scenario, w.Code, w.Body.String())
			}
			if f.store.sessions[session.Principal.SessionID].RevokedAt != nil || leaders.calls != 0 {
				t.Fatal("rejected request revoked session or exposed leadership")
			}
		})
	}
}

func TestAuthStorageFailuresKeepValidSessionsAndReturnSafeErrors(t *testing.T) {
	for _, scenario := range []string{"create", "lookup", "revoke"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			leaders := &fakeLeadership{}
			a := newAuthAPI(t, f, leaders)
			old := fixtureLogin(t, f)
			r := request("GET", Prefix+"/auth/me", "", old.CookieToken, security.Secret{})
			switch scenario {
			case "create":
				f.store.createErr = errors.New("private-driver-canary")
				r = loginRequest()
				r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: old.CookieToken.Reveal()})
			case "lookup":
				f.store.lookupErr = errors.New("private-driver-canary")
			case "revoke":
				f.store.revokeErr = errors.New("private-driver-canary")
				r = request("POST", Prefix+"/auth/logout", "", old.CookieToken, old.CSRFToken)
			}
			w := dispatch(a, r)
			if w.Code != 503 || strings.Contains(w.Body.String(), "canary") || strings.Contains(f.logs.String(), "canary") || w.Header().Get("Set-Cookie") != "" || leaders.calls != 0 {
				t.Fatalf("%s: unsafe outage response: %d %s", scenario, w.Code, w.Body.String())
			}
			if f.store.sessions[old.Principal.SessionID].RevokedAt != nil || len(f.store.sessions) != 1 {
				t.Fatal("storage failure unexpectedly changed sessions")
			}
			f.store.lookupErr = nil
			f.store.createErr = nil
			f.store.revokeErr = nil
			w = dispatch(a, request("GET", Prefix+"/auth/me", "", old.CookieToken, security.Secret{}))
			if w.Code != 200 {
				t.Fatal("outage destroyed valid session")
			}
		})
	}
}

func TestAuthLoginNormalizesUsernameButDoesNotTrimPassword(t *testing.T) {
	f := newFixture(t)
	a := newAuthAPI(t, f, &fakeLeadership{})
	w := dispatch(a, request("POST", Prefix+"/auth/login", `{"username":"  VIEWER  ","password":"correct test password"}`, security.Secret{}, security.Secret{}))
	view := readSession(t, w)
	if view.User.Username != "viewer" {
		t.Fatal("M1 username normalization lost")
	}
	w = dispatch(a, request("POST", Prefix+"/auth/login", `{"username":"viewer","password":" correct test password "}`, security.Secret{}, security.Secret{}))
	if w.Code != 401 {
		t.Fatal("API trimmed password before credential verification")
	}
}
