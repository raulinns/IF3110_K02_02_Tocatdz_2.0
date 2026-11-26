package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const testGroup security.ID = "40000000-0000-4000-8000-000000000001"

type fakeLeadership struct {
	groups []security.ID
	err    error
	calls  int
	userID security.ID
}

func (f *fakeLeadership) LeaderGroups(ctx context.Context, userID security.ID) ([]security.ID, error) {
	f.calls++
	f.userID = userID
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.groups, f.err
}

func newAuthAPI(t *testing.T, f *integrationFixture, leaders GroupLeadership) *API {
	t.Helper()
	cfg := f.cfg
	cfg.Auth = &AuthConfig{Leaders: leaders}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func readSession(t *testing.T, w *httptest.ResponseRecorder) SessionResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected successful auth response, got %d %s", w.Code, w.Body.String())
	}
	var view SessionResponse
	decoder := json.NewDecoder(w.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}

func loginRequest() *http.Request {
	return request("POST", Prefix+"/auth/login", `{"username":"viewer","password":"correct test password"}`, security.Secret{}, security.Secret{})
}

func TestAuthConfigurationRequiresExplicitRealDependencies(t *testing.T) {
	f := newFixture(t)
	var typedNil *fakeLeadership
	for _, leaders := range []GroupLeadership{nil, typedNil} {
		cfg := f.cfg
		cfg.Auth = &AuthConfig{Leaders: leaders}
		if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing leadership accepted")
		}
	}
	for _, field := range []string{"auth", "logout", "cookies"} {
		cfg := f.cfg
		cfg.Auth = &AuthConfig{Leaders: &fakeLeadership{}}
		switch field {
		case "auth":
			cfg.Security.Auth = nil
		case "logout":
			cfg.Security.Logout = nil
		case "cookies":
			cfg.Security.Cookies = nil
		}
		if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatalf("missing %s dependency accepted", field)
		}
	}
	cfg := f.cfg
	cfg.Auth = &AuthConfig{Leaders: &fakeLeadership{}}
	cfg.Routes = []Route{{Method: "GET", Path: "/auth/me", Handle: func(http.ResponseWriter, *http.Request) error { return nil }}}
	if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("custom route replaced built-in auth")
	}
	cfg.Routes[0].Method = "HEAD"
	cfg.Routes[0].Access = Public
	if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("public HEAD route replaced authenticated /me")
	}
	// Config is copied: transport-only callers keep the transport-only behavior.
	a := buildAPI(t, f)
	w := dispatch(a, loginRequest())
	if w.Code != 404 {
		t.Fatal("transport-only API silently enabled an incomplete auth module")
	}
}

type countingAuth struct {
	security.Authenticator
	calls int
}

func (a *countingAuth) Login(ctx context.Context, input security.Credentials, previous security.Secret) (security.SessionResult, error) {
	a.calls++
	return a.Authenticator.Login(ctx, input, previous)
}

func TestAuthLoginInputValidationPrecedesCredentialService(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		field      string
	}{
		{"missing fields", `{}`, 422, "username"},
		{"null username", `{"username":null,"password":"x"}`, 422, "username"},
		{"missing password", `{"username":"viewer"}`, 422, "password"},
		{"empty password", `{"username":"viewer","password":""}`, 422, "password"},
		{"long password", `{"username":"viewer","password":"` + strings.Repeat("x", security.MaxPasswordBytes+1) + `"}`, 422, "password"},
		{"long username", `{"username":"` + strings.Repeat("a", 129) + `","password":"x"}`, 422, "username"},
		{"wrong type", `{"username":12,"password":"x"}`, 400, ""},
		{"duplicate", `{"username":"viewer","username":"other","password":"x"}`, 400, ""},
		{"unknown", `{"username":"viewer","password":"x","admin":true}`, 400, ""},
		{"oversized body", strings.Repeat(" ", int(security.MaxLoginFormBytes)) + `{}`, 413, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			auth := &countingAuth{Authenticator: f.auth}
			f.cfg.Security.Auth = auth
			leaders := &fakeLeadership{}
			a := newAuthAPI(t, f, leaders)
			w := dispatch(a, request("POST", Prefix+"/auth/login", test.body, security.Secret{}, security.Secret{}))
			var body ErrorEnvelope
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != test.status || auth.calls != 0 || leaders.calls != 0 {
				t.Fatalf("%s: %d %s", test.name, w.Code, w.Body.String())
			}
			if test.field != "" && len(body.Error.Fields[test.field]) == 0 {
				t.Fatal("field-specific validation missing")
			}
		})
	}
}

func TestAuthLeadershipProjectionIsFreshDeterministicAndFailClosed(t *testing.T) {
	f := newFixture(t)
	groups := []security.ID{"40000000-0000-4000-8000-000000000002", testGroup, testGroup}
	leaders := &fakeLeadership{groups: groups}
	a := newAuthAPI(t, f, leaders)
	w := dispatch(a, loginRequest())
	cookie := security.NewSecret(w.Result().Cookies()[0].Value)
	view := readSession(t, w)
	if !slices.Equal(view.LeaderOf, []security.ID{testGroup, groups[0]}) || leaders.userID != testUser || !slices.Equal(groups, leaders.groups) {
		t.Fatal("leadership projection incorrect")
	}
	if groups[0] == testGroup {
		t.Fatal("response sorting mutated repository-owned slice")
	}
	leaders.groups = nil
	w = dispatch(a, request("GET", Prefix+"/auth/me", "", cookie, security.Secret{}))
	view = readSession(t, w)
	if view.LeaderOf == nil || len(view.LeaderOf) != 0 {
		t.Fatal("no groups must be [] rather than null/stale values")
	}
	for _, test := range []struct {
		groups []security.ID
		err    error
	}{
		{nil, errors.New("private-dsn-canary")},
		{nil, security.ErrNotFound},
		{[]security.ID{"not-a-uuid"}, nil},
		{[]security.ID{"00000000-0000-0000-0000-000000000000"}, nil},
	} {
		leaders.groups, leaders.err = test.groups, test.err
		w = dispatch(a, request("GET", Prefix+"/auth/me", "", cookie, security.Secret{}))
		if w.Code != 503 || strings.Contains(w.Body.String(), "canary") || strings.Contains(w.Body.String(), `"leader_of"`) {
			t.Fatal("directory failure was exposed or fabricated empty success")
		}
	}
}

func TestAuthLoginCredentialFailuresAreGenericAndKeepPreviousSession(t *testing.T) {
	var expectedMessage string
	for _, scenario := range []string{"wrong password", "unknown user", "disabled account"} {
		f := newFixture(t)
		a := newAuthAPI(t, f, &fakeLeadership{})
		old := fixtureLogin(t, f)
		username, password := "viewer", "wrong-password-canary"
		switch scenario {
		case "unknown user":
			username = "nobody"
		case "disabled account":
			f.store.account.Account.Active = false
		}
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		w := dispatch(a, request("POST", Prefix+"/auth/login", string(body), old.CookieToken, security.Secret{}))
		var envelope ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envelope)
		if w.Code != 401 || envelope.Error.Code != "unauthenticated" || strings.Contains(w.Body.String(), "canary") || w.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s: unsafe failure %d %s", scenario, w.Code, w.Body.String())
		}
		if expectedMessage == "" {
			expectedMessage = envelope.Error.Message
		} else if expectedMessage != envelope.Error.Message {
			t.Fatal("credential errors reveal account existence/state")
		}
		if f.store.sessions[old.Principal.SessionID].RevokedAt != nil || len(f.store.sessions) != 1 {
			t.Fatal("failed login changed existing sessions")
		}
		if strings.Contains(f.logs.String(), password) || strings.Contains(f.logs.String(), old.CookieToken.Reveal()) || strings.Contains(f.logs.String(), string(old.Principal.SessionID)) {
			t.Fatal("auth audit leaked sensitive identity")
		}
	}
}

func TestAuthLoginKeepsAccountAndPeerIPRateLimits(t *testing.T) {
	for _, byAccount := range []bool{true, false} {
		f := newFixture(t)
		a := newAuthAPI(t, f, &fakeLeadership{})
		attempts := 21
		if byAccount {
			attempts = 6
		}
		for i := 0; i < attempts; i++ {
			username := "viewer"
			if !byAccount {
				username = fmt.Sprintf("missing_%02d", i)
			}
			body, _ := json.Marshal(map[string]string{"username": username, "password": "wrong-password"})
			r := request("POST", Prefix+"/auth/login", string(body), security.Secret{}, security.Secret{})
			if byAccount {
				r.RemoteAddr = fmt.Sprintf("192.0.2.%d:12345", i+1)
			}
			r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
			w := dispatch(a, r)
			want := 401
			if i == attempts-1 {
				want = 429
			}
			if w.Code != want || (want == 429 && w.Header().Get("Retry-After") != "60") {
				t.Fatalf("account=%v attempt=%d: %d", byAccount, i, w.Code)
			}
		}
	}
}

func TestAuthRouteMethodsAndHeadUseBuiltInSecurity(t *testing.T) {
	f := newFixture(t)
	a := newAuthAPI(t, f, &fakeLeadership{})
	for path, allow := range map[string]string{"/auth/login": "POST", "/auth/logout": "POST", "/auth/me": "GET, HEAD"} {
		method := "GET"
		if path == "/auth/me" {
			method = "POST"
		}
		w := dispatch(a, request(method, Prefix+path, "", security.Secret{}, security.Secret{}))
		if w.Code != 405 || w.Header().Get("Allow") != allow {
			t.Fatalf("%s: %d allow=%s", path, w.Code, w.Header().Get("Allow"))
		}
	}
	w := dispatch(a, request("HEAD", Prefix+"/auth/me", "", security.Secret{}, security.Secret{}))
	if w.Code != 401 || w.Body.Len() != 0 {
		t.Fatal("HEAD /me bypassed authentication or wrote body")
	}
	session := fixtureLogin(t, f)
	w = dispatch(a, request("HEAD", Prefix+"/auth/me", "", session.CookieToken, security.Secret{}))
	if w.Code != 200 || w.Body.Len() != 0 || len(w.Result().Cookies()) != 1 {
		t.Fatal("HEAD /me changed session middleware semantics")
	}
}
