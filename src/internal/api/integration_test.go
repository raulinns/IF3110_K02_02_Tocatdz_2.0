package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	testOrigin               = "https://platform.example"
	testUser     security.ID = "30000000-0000-4000-8000-000000000001"
	testDatabase security.ID = "10000000-0000-4000-8000-000000000001"
)

type integrationClock struct{ now time.Time }

func (c *integrationClock) Now() time.Time { return c.now }

// Only persistence and the expensive KDF are faked; session service, token
// encryption, CSRF, cookies, limiter, middleware and authorization are real.
type integrationStore struct {
	mu       sync.Mutex
	account  security.CredentialRecord
	sessions map[security.ID]security.Session
	access   security.DatabaseAccess
}

func (s *integrationStore) FindByUsername(_ context.Context, username string) (security.CredentialRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account.Account.Username != username {
		return security.CredentialRecord{}, security.ErrNotFound
	}
	return s.account, nil
}
func (s *integrationStore) FindAccount(_ context.Context, id security.ID) (security.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.account.Account.ID {
		return security.Account{}, security.ErrNotFound
	}
	return s.account.Account, nil
}
func (s *integrationStore) Rehash(_ context.Context, id security.ID, expected, replacement security.Secret, version uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.account.Account.ID || expected.Reveal() != s.account.PasswordHash.Reveal() || version != s.account.Account.AuthVersion {
		return security.ErrConflict
	}
	s.account.PasswordHash = replacement
	return nil
}
func (s *integrationStore) Create(_ context.Context, session security.Session, version uint64, previous *security.TokenDigest, _ security.SessionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.account.Account.Active || version != s.account.Account.AuthVersion {
		return security.ErrConflict
	}
	if previous != nil {
		for id, old := range s.sessions {
			if old.CurrentDigest == *previous {
				at := session.CreatedAt
				old.RevokedAt = &at
				old.Revision++
				s.sessions[id] = old
			}
		}
	}
	s.sessions[session.ID] = session
	return nil
}
func (s *integrationStore) Lookup(_ context.Context, digest security.TokenDigest) (security.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.sessions {
		if session.CurrentDigest == digest {
			return security.SessionSnapshot{Session: session, Account: s.account.Account}, nil
		}
		if session.PreviousDigest != nil && *session.PreviousDigest == digest {
			return security.SessionSnapshot{Session: session, Account: s.account.Account, PresentedPrevious: true}, nil
		}
	}
	return security.SessionSnapshot{}, security.ErrNotFound
}
func (s *integrationStore) FindByID(_ context.Context, id security.ID) (security.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return security.SessionSnapshot{}, security.ErrNotFound
	}
	return security.SessionSnapshot{Session: session, Account: s.account.Account}, nil
}
func (s *integrationStore) Advance(_ context.Context, digest security.TokenDigest, next security.Session, revision, version uint64, _ security.SessionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.sessions[next.ID]
	if !ok || old.Revision != revision || old.RevokedAt != nil || !s.account.Account.Active || s.account.Account.AuthVersion != version || (old.CurrentDigest != digest && (old.PreviousDigest == nil || *old.PreviousDigest != digest)) {
		return security.ErrConflict
	}
	next.Revision = old.Revision + 1
	s.sessions[next.ID] = next
	return nil
}
func (s *integrationStore) Revoke(_ context.Context, id security.ID, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		session.RevokedAt = &now
		session.Revision++
		s.sessions[id] = session
	}
	return nil
}
func (s *integrationStore) AccessForDatabase(_ context.Context, user, database security.ID) (security.AccessSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if user != s.account.Account.ID || database != testDatabase {
		return security.AccessSnapshot{}, security.ErrNotFound
	}
	return security.AccessSnapshot{Account: s.account.Account, GroupAccess: []security.DatabaseAccess{s.access}}, nil
}

type integrationFixture struct {
	cfg        Config
	auth       *security.AuthService
	store      *integrationStore
	clock      *integrationClock
	authorizer *security.BackendAuthorizer
	logs       bytes.Buffer
}

func newFixture(t *testing.T) *integrationFixture {
	t.Helper()
	f := &integrationFixture{clock: &integrationClock{now: time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)}}
	// Injecting a fast KDF keeps these transport tests independent of Argon2 costs.
	hasher, err := security.NewArgon2idHasher(func(password, salt []byte, _, _ uint32, _ uint8, _ uint32) []byte {
		sum := sha256.Sum256(append(append([]byte(nil), password...), salt...))
		return sum[:]
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(context.Background(), security.NewSecret("correct test password"))
	if err != nil {
		t.Fatal(err)
	}
	f.store = &integrationStore{account: security.CredentialRecord{Account: security.Account{ID: testUser, Username: "viewer", Active: true, AuthVersion: 1}, PasswordHash: hash}, sessions: map[security.ID]security.Session{}, access: security.AccessViewOnly}
	key := func(fill byte) security.Secret {
		return security.NewSecret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
	}
	protector, err := security.NewAESGCMTokenProtector(key(1))
	if err != nil {
		t.Fatal(err)
	}
	csrf, err := security.NewHMACCSRFGuard(testOrigin, key(2))
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := security.NewLoginLimiter(f.clock)
	if err != nil {
		t.Fatal(err)
	}
	f.auth, err = security.NewAuthService(context.Background(), security.AuthServiceConfig{Credentials: f.store, Sessions: f.store, Passwords: hasher, Tokens: security.NewTokenGenerator(), Protector: protector, CSRF: csrf, Throttle: limiter, Clock: f.clock, Policy: security.DefaultSessionPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := security.NewSessionCookies(testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = Config{Logger: slog.New(slog.NewJSONHandler(&f.logs, nil)), Security: security.HTTPMiddlewareConfig{Auth: f.auth, Logout: f.auth, CSRF: csrf, Cookies: cookies, Throttle: limiter}}
	f.authorizer, err = security.NewBackendAuthorizer(f.store, f.store, f.clock, security.DefaultSessionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func buildAPI(t *testing.T, f *integrationFixture, routes ...Route) *API {
	t.Helper()
	cfg := f.cfg
	cfg.Routes = routes
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func request(method, target, body string, cookie, csrf security.Secret) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:12345"
	if !security.SafeMethod(method) {
		r.Header.Set("Origin", testOrigin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if !cookie.Empty() {
		r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: cookie.Reveal()})
	}
	if !csrf.Empty() {
		r.Header.Set(security.CSRFHeaderName, csrf.Reveal())
	}
	return r
}

func dispatch(handler http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func fixtureLogin(t *testing.T, f *integrationFixture) security.SessionResult {
	t.Helper()
	session, err := f.auth.Login(context.Background(), security.Credentials{Username: "viewer", Password: security.NewSecret("correct test password")}, security.Secret{})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestJSONFoundationIntegratesRealSessionRotationCSRFAndAuthorization(t *testing.T) {
	f := newFixture(t)
	session := fixtureLogin(t, f)
	writes := 0
	a := buildAPI(t, f,
		Route{Method: "GET", Path: "/session", Handle: func(w http.ResponseWriter, r *http.Request) error {
			identity, ok := security.IdentityFromContext(r.Context())
			if !ok {
				return security.ErrUnauthenticated
			}
			token, _ := security.CSRFTokenFromContext(r.Context())
			return WriteJSON(w, 200, struct {
				User string `json:"user"`
				CSRF string `json:"csrf_token"`
			}{identity.Username, token.Reveal()})
		}},
		Route{Method: "POST", Path: "/write", Body: JSONBody, Handle: func(w http.ResponseWriter, r *http.Request) error {
			var input exampleInput
			if err := DecodeJSON(w, r, &input); err != nil {
				return err
			}
			principal, _ := security.PrincipalFromContext(r.Context())
			if err := f.authorizer.RequireDatabase(r.Context(), principal, testDatabase, security.ActionCreateMigration); err != nil {
				return err
			}
			writes++
			return WriteJSON(w, 200, map[string]string{"title": input.Title})
		}},
	)
	// Trigger genuine rotation in the auth service; cookie must survive JSON output.
	f.clock.now = f.clock.now.Add(security.DefaultSessionPolicy().RotationInterval)
	w := dispatch(a, request("GET", Prefix+"/session", "", session.CookieToken, security.Secret{}))
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Value == session.CookieToken.Reveal() {
		t.Fatalf("rotation failed: %d %s", w.Code, w.Body.String())
	}
	cookie := security.NewSecret(w.Result().Cookies()[0].Value)
	var view struct {
		CSRF string `json:"csrf_token"`
	}
	if json.Unmarshal(w.Body.Bytes(), &view) != nil {
		t.Fatal("invalid JSON view")
	}
	csrf := security.NewSecret(view.CSRF)
	w = dispatch(a, request("POST", Prefix+"/write", `{"title":"valid"}`, cookie, csrf))
	if w.Code != 403 || writes != 0 || !strings.Contains(w.Body.String(), `"code":"forbidden"`) {
		t.Fatal("viewer wrote or JSON body was consumed by CSRF")
	}
	f.store.access = security.AccessViewEdit
	w = dispatch(a, request("POST", Prefix+"/write", `{"title":"valid"}`, cookie, csrf))
	if w.Code != 200 || writes != 1 || !strings.Contains(w.Body.String(), `"title":"valid"`) {
		t.Fatal("authorized JSON request failed")
	}
	for _, scenario := range []string{"missing CSRF", "duplicate CSRF", "query token", "foreign origin", "missing cookie", "expired"} {
		r := request("POST", Prefix+"/write", `{"title":"valid"}`, cookie, csrf)
		want := 403
		switch scenario {
		case "missing CSRF":
			r.Header.Del(security.CSRFHeaderName)
		case "duplicate CSRF":
			r.Header.Add(security.CSRFHeaderName, csrf.Reveal())
		case "query token":
			r.URL.RawQuery = "csrf_token=" + csrf.Reveal()
		case "foreign origin":
			r.Header.Set("Origin", "https://attacker.example")
		case "missing cookie":
			r.Header.Del("Cookie")
			want = 401
		case "expired":
			f.clock.now = f.clock.now.Add(security.DefaultSessionPolicy().IdleTimeout)
			want = 401
		}
		w = dispatch(a, r)
		if w.Code != want || writes != 1 {
			t.Fatalf("%s: %d %s", scenario, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), cookie.Reveal()) || strings.Contains(w.Body.String(), csrf.Reveal()) {
			t.Fatal("security error leaked token")
		}
	}
	if strings.Contains(f.logs.String(), cookie.Reveal()) {
		t.Fatal("logger leaked session")
	}
}

func TestJSONLoginGuardIntegratesLimiterAndPreservesSmallerBodyLimit(t *testing.T) {
	f := newFixture(t)
	calls := 0
	a := buildAPI(t, f, Route{Method: "POST", Path: "/auth/login", Access: Login, Body: JSONBody, Handle: func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		calls++
		result, err := f.auth.Login(r.Context(), security.Credentials{Username: input.Username, Password: security.NewSecret(input.Password)}, security.Secret{})
		if err != nil {
			return err
		}
		if err := f.cfg.Security.Cookies.Write(w, result.CookieToken); err != nil {
			return err
		}
		return WriteJSON(w, 200, map[string]string{"csrf_token": result.CSRFToken.Reveal()})
	}})
	w := dispatch(a, request("POST", Prefix+"/auth/login", `{"username":"viewer","password":"correct test password"}`, security.Secret{}, security.Secret{}))
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || calls != 1 {
		t.Fatal("JSON login guard rejected valid bootstrap")
	}
	w = dispatch(a, request("POST", Prefix+"/auth/login", `{"username":"viewer","password":"wrong-password-canary"}`, security.Secret{}, security.Secret{}))
	if w.Code != 401 || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("invalid credentials response unsafe")
	}
	r := request("POST", Prefix+"/auth/login", strings.Repeat(" ", int(security.MaxLoginFormBytes))+`{}`, security.Secret{}, security.Secret{})
	r.ContentLength = -1
	w = dispatch(a, r)
	if w.Code != 413 || calls != 2 {
		t.Fatalf("chunked login body cap: %d", w.Code)
	}
	for i := 0; i < 20; i++ {
		_ = f.cfg.Security.Throttle.CheckIP(context.Background(), "192.0.2.1")
	}
	w = dispatch(a, request("POST", Prefix+"/auth/login", `{}`, security.Secret{}, security.Secret{}))
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" || calls != 2 {
		t.Fatal("JSON transport bypassed login IP limiter")
	}
}

// Compile-time checks keep this test fake faithful to the production ports.
var _ security.CredentialRepository = (*integrationStore)(nil)
var _ security.SessionRepository = (*integrationStore)(nil)
var _ security.AccessRepository = (*integrationStore)(nil)
