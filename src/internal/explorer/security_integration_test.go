package explorer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type integrationClock struct{ now time.Time }

func (c integrationClock) Now() time.Time { return c.now }

type integrationStore struct {
	mu      sync.Mutex
	session security.Session
	account security.Account
	grants  map[security.ID][]security.DatabaseAccess
}

func (s *integrationStore) FindByUsername(context.Context, string) (security.CredentialRecord, error) {
	return security.CredentialRecord{}, security.ErrNotFound
}
func (s *integrationStore) FindAccount(context.Context, security.ID) (security.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account, nil
}
func (s *integrationStore) Rehash(context.Context, security.ID, security.Secret, security.Secret, uint64) error {
	return security.ErrUnavailable
}
func (s *integrationStore) Create(context.Context, security.Session, uint64, *security.TokenDigest, security.SessionPolicy) error {
	return security.ErrUnavailable
}

func (s *integrationStore) Lookup(ctx context.Context, digest security.TokenDigest) (security.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return security.SessionSnapshot{}, err
	}
	previous := s.session.PreviousDigest != nil && digest == *s.session.PreviousDigest
	if digest != s.session.CurrentDigest && !previous {
		return security.SessionSnapshot{}, security.ErrNotFound
	}
	return security.SessionSnapshot{Session: s.session, Account: s.account, PresentedPrevious: previous}, nil
}

func (s *integrationStore) FindByID(ctx context.Context, id security.ID) (security.SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return security.SessionSnapshot{}, err
	}
	if id != s.session.ID {
		return security.SessionSnapshot{}, security.ErrNotFound
	}
	return security.SessionSnapshot{Session: s.session, Account: s.account}, nil
}

func (s *integrationStore) Advance(ctx context.Context, _ security.TokenDigest, replacement security.Session, revision, version uint64, _ security.SessionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.session.RevokedAt != nil || !s.account.Active || s.session.Revision != revision || s.account.AuthVersion != version {
		return security.ErrConflict
	}
	replacement.Revision = revision + 1
	s.session = replacement
	return nil
}

func (s *integrationStore) Revoke(_ context.Context, id security.ID, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.session.ID {
		s.session.RevokedAt = &now
		s.session.Revision++
	}
	return nil
}

func (s *integrationStore) AccessForDatabase(ctx context.Context, _ security.ID, id security.ID) (security.AccessSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return security.AccessSnapshot{}, err
	}
	return security.AccessSnapshot{Account: s.account, GroupAccess: append([]security.DatabaseAccess(nil), s.grants[id]...)}, nil
}

type integrationFixture struct {
	mux    *http.ServeMux
	reader *readerStub
	store  *integrationStore
	cookie security.Secret
}

func newIntegrationFixture(t *testing.T) integrationFixture {
	t.Helper()
	f := newFixture(t)
	clock := integrationClock{now: time.Now().UTC()}
	key := func(fill byte) security.Secret {
		return security.NewSecret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
	}
	protector, _ := security.NewAESGCMTokenProtector(key(1))
	csrf, _ := security.NewHMACCSRFGuard("https://platform.example", key(2))
	tokens := security.NewTokenGenerator()
	token, err := tokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := security.DigestToken(token)
	sealed, err := protector.Seal(context.Background(), token, sessionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	store := &integrationStore{
		account: security.Account{ID: userID, Username: "alice", Active: true, PlatformAdmin: true, AuthVersion: 1},
		session: security.Session{ID: sessionID, UserID: userID, AuthVersion: 1, Revision: 1, Generation: 1, CSRFEpoch: 1,
			CreatedAt: clock.now, LastSeenAt: clock.now, RotatedAt: clock.now, AbsoluteExpiresAt: clock.now.Add(12 * time.Hour), CurrentDigest: digest, CurrentTokenSealed: sealed},
		grants: map[security.ID][]security.DatabaseAccess{databaseID: {security.AccessViewOnly}},
	}
	// KDF dan repository di sini hanya fixture, sedangkan guard, crypto, authorizer, dan service memakai implementasi asli.
	passwords, _ := security.NewArgon2idHasher(func(password, salt []byte, _ uint32, _ uint32, _ uint8, _ uint32) []byte {
		value := sha256.Sum256(append(append([]byte(nil), password...), salt...))
		return value[:]
	})
	throttle, _ := security.NewLoginLimiter(clock)
	auth, err := security.NewAuthService(context.Background(), security.AuthServiceConfig{Credentials: store, Sessions: store, Passwords: passwords,
		Tokens: tokens, Protector: protector, CSRF: csrf, Throttle: throttle, Clock: clock, Policy: security.DefaultSessionPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	cookies, _ := security.NewSessionCookies("https://platform.example", true)
	guards, err := security.NewHTTPMiddleware(security.HTTPMiddlewareConfig{Auth: auth, Logout: auth, CSRF: csrf, Cookies: cookies, Throttle: throttle})
	if err != nil {
		t.Fatal(err)
	}
	access, _ := security.NewBackendAuthorizer(store, store, clock, security.DefaultSessionPolicy())
	service, err := NewService(Config{Directory: f.directory, Reader: f.reader, Authorizer: access})
	if err != nil {
		t.Fatal(err)
	}
	var viewer Viewer = service
	mux := http.NewServeMux()
	encode := func(w http.ResponseWriter, value any, err error) {
		if err != nil {
			security.WriteHTTPError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	req := func(r *http.Request) Request {
		p, _ := security.PrincipalFromContext(r.Context())
		return Request{Principal: p, DatabaseID: security.ID(r.PathValue("databaseID"))}
	}
	mux.Handle("GET /databases", guards.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := security.PrincipalFromContext(r.Context())
		value, err := viewer.ListAccessible(r.Context(), p)
		encode(w, value, err)
	})))
	mux.Handle("GET /databases/{databaseID}/schema", guards.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := viewer.Schemas(r.Context(), req(r))
		encode(w, value, err)
	})))
	mux.Handle("GET /databases/{databaseID}/tables/{schema}/{table}", guards.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := viewer.Table(r.Context(), req(r), r.PathValue("schema"), r.PathValue("table"))
		encode(w, value, err)
	})))
	mux.Handle("GET /databases/{databaseID}/tables/{schema}/{table}/rows", guards.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, err := ParseRowsQuery(r.PathValue("schema"), r.PathValue("table"), r.URL.Query())
		if err != nil {
			security.WriteHTTPError(w, err)
			return
		}
		value, err := viewer.Rows(r.Context(), req(r), query)
		encode(w, value, err)
	})))
	return integrationFixture{mux: mux, reader: f.reader, store: store, cookie: token}
}

func (f integrationFixture) get(path string, authenticated bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if authenticated {
		r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: f.cookie.Reveal()})
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func TestExplorerHTTPAndSecurityIntegration(t *testing.T) {
	f := newIntegrationFixture(t)
	w := f.get("/databases", false)
	if w.Code != 401 || f.reader.calls != 0 {
		t.Fatal("anonymous explorer opened target reader")
	}
	w = f.get("/databases", true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Hidden") || !strings.Contains(w.Body.String(), "App") {
		t.Fatal("database directory exposed unauthorized entry")
	}
	for _, path := range []string{
		"/databases/" + string(databaseID) + "/schema",
		"/databases/" + string(databaseID) + "/tables/public/items",
		"/databases/" + string(databaseID) + "/tables/public/items/rows?page=1&limit=2",
	} {
		w = f.get(path, true)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), f.cookie.Reveal()) {
			t.Fatal("View Only explorer read failed or session leaked")
		}
	}
	var page RowsPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || !page.HasNext || len(page.Rows) != 2 || !page.Rows[1][1].Null {
		t.Fatal("UI rows DTO differs from service contract")
	}
	before := f.reader.calls
	w = f.get("/databases/"+string(deniedID)+"/schema", true)
	if w.Code != 403 || f.reader.calls != before {
		t.Fatal("admin bypassed group grants on another database")
	}
	w = f.get("/databases/"+string(databaseID)+"/tables/pg_catalog/pg_class", true)
	if w.Code != 404 || f.reader.calls != before {
		t.Fatal("system catalog target was queried")
	}
	_ = f.store.Revoke(context.Background(), sessionID, time.Now())
	w = f.get("/databases/"+string(databaseID)+"/schema", true)
	if w.Code != 401 || f.reader.calls != before {
		t.Fatal("revoked user still read target schema")
	}
}

func TestExplorerHTTPFailureAndBoundsIntegration(t *testing.T) {
	f := newIntegrationFixture(t)
	w := f.get("/databases/"+string(databaseID)+"/tables/public/items/rows?limit=201", true)
	if w.Code != 400 || f.reader.calls != 0 {
		t.Fatal("invalid pagination reached target adapter")
	}
	f.reader.err = errors.New("postgres://alice:private-DSN-password@internal/database")
	w = f.get("/databases/"+string(databaseID)+"/tables/public/items/rows", true)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private-DSN-password") {
		t.Fatal("connection failure leaked driver credentials")
	}
	f.reader.err = nil
	f.reader.batch = RowBatch{Columns: []string{"value"}, Rows: make([][]Cell, MaxLimit)}
	for i := range f.reader.batch.Rows {
		f.reader.batch.Rows[i] = []Cell{{Text: strings.Repeat("&", MaxCellBytes)}}
	}
	w = f.get("/databases/"+string(databaseID)+"/tables/public/items/rows?limit=200", true)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Hasil terlalu besar") {
		t.Fatal("oversized result did not use shared UI error mapping")
	}
}
