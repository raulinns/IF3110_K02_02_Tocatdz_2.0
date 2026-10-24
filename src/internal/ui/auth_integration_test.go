package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	testOrigin             = "http://localhost:8080"
	testUserID security.ID = "b0c4f0c0-9167-43d6-9089-afd7b9826f0d"
)

var csrfField = regexp.MustCompile(`name="csrf_token" value="([A-Za-z0-9_-]{43})"`)

type memoryStore struct {
	mu       sync.Mutex
	accounts map[security.ID]security.CredentialRecord
	sessions map[security.ID]security.Session
	grants   map[security.ID][]security.DatabaseAccess
}

func (s *memoryStore) AccessForDatabase(_ context.Context, user, database security.ID) (security.AccessSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return security.AccessSnapshot{Account: s.accounts[user].Account, GroupAccess: append([]security.DatabaseAccess(nil), s.grants[database]...)}, nil
}

func (s *memoryStore) grant(database security.ID, access ...security.DatabaseAccess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[database] = access
}

func (s *memoryStore) FindByUsername(_ context.Context, username string) (security.CredentialRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.accounts {
		if record.Account.Username == username {
			return record, nil
		}
	}
	return security.CredentialRecord{}, security.ErrNotFound
}

func (s *memoryStore) FindAccount(_ context.Context, id security.ID) (security.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.accounts[id]
	if !ok {
		return security.Account{}, security.ErrNotFound
	}
	return record.Account, nil
}

func (s *memoryStore) Rehash(context.Context, security.ID, security.Secret, security.Secret, uint64) error {
	return nil
}

func (s *memoryStore) find(digest security.TokenDigest) (security.Session, bool, bool) {
	for _, session := range s.sessions {
		if session.CurrentDigest == digest {
			return session, false, true
		}
		if session.PreviousDigest != nil && *session.PreviousDigest == digest {
			return session, true, true
		}
	}
	return security.Session{}, false, false
}

func (s *memoryStore) Create(_ context.Context, session security.Session, version uint64, previous *security.TokenDigest, _ security.SessionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	account := s.accounts[session.UserID].Account
	if !account.Active || account.AuthVersion != version {
		return security.ErrConflict
	}
	if previous != nil {
		if old, _, ok := s.find(*previous); ok && old.RevokedAt == nil {
			now := session.CreatedAt
			old.RevokedAt = &now
			old.Revision++
			s.sessions[old.ID] = old
		}
	}
	s.sessions[session.ID] = session
	return nil
}

func (s *memoryStore) Lookup(_ context.Context, digest security.TokenDigest) (security.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, previous, ok := s.find(digest)
	if !ok {
		return security.SessionSnapshot{}, security.ErrNotFound
	}
	return security.SessionSnapshot{Session: session, Account: s.accounts[session.UserID].Account, PresentedPrevious: previous}, nil
}

func (s *memoryStore) FindByID(_ context.Context, id security.ID) (security.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return security.SessionSnapshot{}, security.ErrNotFound
	}
	return security.SessionSnapshot{Session: session, Account: s.accounts[session.UserID].Account}, nil
}

func (s *memoryStore) Advance(_ context.Context, _ security.TokenDigest, replacement security.Session, revision, _ uint64, _ security.SessionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[replacement.ID]
	if !ok || current.Revision != revision || current.RevokedAt != nil {
		return security.ErrConflict
	}
	replacement.Revision = current.Revision + 1
	s.sessions[replacement.ID] = replacement
	return nil
}

func (s *memoryStore) Revoke(_ context.Context, id security.ID, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok && session.RevokedAt == nil {
		session.RevokedAt = &now
		session.Revision++
		s.sessions[id] = session
	}
	return nil
}

func testKeyDeriver(password, salt []byte, _, _ uint32, _ uint8, _ uint32) []byte {
	key := sha256.Sum256(append(append([]byte(nil), password...), salt...))
	return key[:]
}

func testKey(fill byte) security.Secret {
	return security.NewSecret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

type integrationEnv struct {
	mux        *http.ServeMux
	store      *memoryStore
	explorer   *memoryExplorer
	migrations *memoryMigrations
	admin      *memoryAdmin
}

func newIntegrationServer(t *testing.T) *http.ServeMux { return newIntegrationEnv(t).mux }

func newIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()
	ctx := context.Background()
	hasher, err := security.NewArgon2idHasher(testKeyDeriver)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(ctx, security.NewSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{
		accounts: map[security.ID]security.CredentialRecord{
			testUserID: {Account: security.Account{ID: testUserID, Username: "budi", Active: true, AuthVersion: 1}, PasswordHash: hash},
		},
		sessions: map[security.ID]security.Session{},
		grants:   map[security.ID][]security.DatabaseAccess{},
	}
	protector, err := security.NewAESGCMTokenProtector(testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	csrf, err := security.NewHMACCSRFGuard(testOrigin, testKey(2))
	if err != nil {
		t.Fatal(err)
	}
	throttle, err := security.NewLoginLimiter(security.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := security.NewAuthService(ctx, security.AuthServiceConfig{
		Credentials: store, Sessions: store, Passwords: hasher, Tokens: security.NewTokenGenerator(), Protector: protector,
		CSRF: csrf, Throttle: throttle, Clock: security.SystemClock{}, Policy: security.DefaultSessionPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := security.NewSessionCookies(testOrigin, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	guard, err := security.NewHTTPMiddleware(security.HTTPMiddlewareConfig{
		Auth: auth, Logout: auth, CSRF: csrf, Cookies: cookies, Throttle: throttle, ErrorWriter: renderer.WriteError,
	})
	if err != nil {
		t.Fatal(err)
	}
	authUI, err := NewAuthUI(auth, guard, cookies, renderer)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := security.NewBackendAuthorizer(store, store, security.SystemClock{}, security.DefaultSessionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	databaseGuard, err := NewDatabaseGuard(authorizer)
	if err != nil {
		t.Fatal(err)
	}
	databaseUI, err := NewDatabaseUI(&memoryCatalog{store: store}, guard, renderer)
	if err != nil {
		t.Fatal(err)
	}
	env := &integrationEnv{
		mux: http.NewServeMux(), store: store, explorer: &memoryExplorer{},
		migrations: &memoryMigrations{records: map[security.ID]memoryMigration{}}, admin: &memoryAdmin{},
	}
	explorerUI, err := NewExplorerUI(env.explorer, guard, databaseGuard, renderer)
	if err != nil {
		t.Fatal(err)
	}
	migrationUI, err := NewMigrationUI(env.migrations, guard, databaseGuard, renderer)
	if err != nil {
		t.Fatal(err)
	}
	adminUI, err := NewAdminUI(AdminServices{Users: env.admin, Groups: env.admin, Connections: env.admin, Grants: env.admin}, guard, databaseGuard, renderer)
	if err != nil {
		t.Fatal(err)
	}
	for _, registrar := range []security.RouteRegistrar{authUI, databaseUI, explorerUI, migrationUI, adminUI} {
		registrar.RegisterRoutes(env.mux)
	}
	return env
}

func browserRequest(method, target, body string, cookie security.Secret) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if method == http.MethodPost {
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if !cookie.Empty() {
		r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: cookie.Reveal()})
	}
	return r
}

func TestLoginPageToLogoutIntegration(t *testing.T) {
	mux := newIntegrationServer(t)
	dispatch := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := dispatch(browserRequest("GET", "/login", "", security.Secret{})); w.Code != http.StatusOK {
		t.Fatal("login page unavailable")
	}
	w := dispatch(browserRequest("POST", "/login", loginForm("budi", "password yang salah sekali"), security.Secret{}))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Username atau password tidak valid") || len(w.Result().Cookies()) != 0 {
		t.Fatal("wrong password not rejected on login page")
	}
	foreign := browserRequest("POST", "/login", loginForm("budi", testPassword), security.Secret{})
	foreign.Header.Set("Origin", "https://evil.example")
	if w := dispatch(foreign); w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
		t.Fatal("cross-origin login accepted")
	}
	w = dispatch(browserRequest("POST", "/login", loginForm(" BUDI ", testPassword), security.Secret{}))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/databases" || len(w.Result().Cookies()) != 1 {
		t.Fatal("valid login rejected")
	}
	cookie := security.NewSecret(w.Result().Cookies()[0].Value)
	w = dispatch(browserRequest("GET", "/databases", "", cookie))
	body := w.Body.String()
	match := csrfField.FindStringSubmatch(body)
	if w.Code != http.StatusOK || !strings.Contains(body, "budi") || match == nil {
		t.Fatal("protected page missing session identity or logout CSRF token")
	}
	if strings.Contains(body, cookie.Reveal()) || strings.Contains(body, testPassword) {
		t.Fatal("session cookie or password leaked into page")
	}
	if w := dispatch(browserRequest("POST", "/logout", "", cookie)); w.Code != http.StatusForbidden {
		t.Fatal("logout without CSRF token accepted")
	}
	w = dispatch(browserRequest("POST", "/logout", url.Values{"csrf_token": {match[1]}}.Encode(), cookie))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatal("logout failed")
	}
	if w := dispatch(browserRequest("GET", "/databases", "", cookie)); w.Code != http.StatusUnauthorized {
		t.Fatal("revoked session still accepted")
	}
}
