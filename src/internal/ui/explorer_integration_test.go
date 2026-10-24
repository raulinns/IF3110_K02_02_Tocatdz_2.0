package ui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
)

type explorerFixture struct {
	store   *memoryStore
	granted bool
	access  security.DatabaseAccess
	reads   int
	failure error
	onRead  func()
	options explorer.RowReadOptions
}

func (f *explorerFixture) AccessForDatabase(_ context.Context, user, database security.ID) (security.AccessSnapshot, error) {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	access := security.AccessNone
	if f.granted && database == testDatabaseA {
		access = security.AccessViewOnly
		if f.access != "" {
			access = f.access
		}
	}
	return security.AccessSnapshot{Account: f.store.accounts[user].Account, GroupAccess: []security.DatabaseAccess{access}}, nil
}

func (f *explorerFixture) ListAccessible(context.Context, security.Principal) ([]explorer.DatabaseSummary, error) {
	return []explorer.DatabaseSummary{
		{ID: testDatabaseA, Name: "inventaris", Access: security.AccessViewOnly},
		{ID: testDatabaseB, Name: "database-rahasia", Access: security.AccessNone},
	}, nil
}

func (f *explorerFixture) Schemas(context.Context, security.ID) ([]explorer.Schema, error) {
	f.reads++
	return []explorer.Schema{
		{Name: "pg_catalog", Tables: []explorer.TableSummary{{Name: "pg_authid", Kind: explorer.KindTable}}},
		{Name: "public", Tables: []explorer.TableSummary{{Name: "users", Kind: explorer.KindTable}, {Name: "a/b", Kind: explorer.KindTable}}},
	}, f.failure
}

func (f *explorerFixture) Table(_ context.Context, _ security.ID, schema, name string) (explorer.Table, error) {
	f.reads++
	def := "gen_random_uuid()"
	return explorer.Table{Schema: schema, Name: name, Kind: explorer.KindTable,
		Columns:     []explorer.Column{{Name: "id", DataType: "uuid", Default: &def}, {Name: "email", DataType: "text", Nullable: true}},
		Constraints: []explorer.Constraint{{Name: "users_pkey", Type: "PRIMARY KEY", Definition: "PRIMARY KEY (id)"}},
		Indexes:     []explorer.Index{{Name: "users_pkey", Definition: "CREATE UNIQUE INDEX users_pkey ON users (id)", Unique: true, Primary: true}},
	}, f.failure
}

func (f *explorerFixture) Rows(_ context.Context, _ security.ID, _, _ string, options explorer.RowReadOptions) (explorer.RowBatch, error) {
	f.reads++
	f.options = options
	if f.onRead != nil {
		f.onRead()
	}
	row := []explorer.Cell{{Text: "<script>canary-cell</script>"}, {Null: true}, {Text: ""}, {Text: `\x1234`, Encoding: explorer.EncodingHex, Truncated: true}}
	return explorer.RowBatch{Columns: []string{"text", "null", "empty", "binary"}, Rows: [][]explorer.Cell{row, row, row}, Order: explorer.OrderBestEffort}, f.failure
}

func newExplorerIntegration(t *testing.T) (http.Handler, *explorerFixture) {
	t.Helper()
	cfg, store := newIntegrationSecurity(t)
	fixture := &explorerFixture{store: store, granted: true}
	authorizer, err := security.NewBackendAuthorizer(store, fixture, security.SystemClock{}, security.DefaultSessionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := explorer.NewService(explorer.Config{Directory: fixture, Reader: fixture, Authorizer: authorizer})
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := NewFrontend(FrontendConfig{Security: cfg, Viewer: viewer, Authorizer: authorizer})
	if err != nil {
		t.Fatal(err)
	}
	return web.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), frontend), fixture
}

func newIntegrationServer(t *testing.T) http.Handler {
	handler, _ := newExplorerIntegration(t)
	return handler
}

func dispatchFrontend(handler http.Handler, method, target, body string, cookie security.Secret) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, browserRequest(method, target, body, cookie))
	return w
}

func loginFrontend(t *testing.T, handler http.Handler) security.Secret {
	t.Helper()
	w := dispatchFrontend(handler, "POST", "/login", loginForm("budi", testPassword), security.Secret{})
	if w.Code != http.StatusSeeOther || len(w.Result().Cookies()) != 1 {
		t.Fatal("login failed")
	}
	return security.NewSecret(w.Result().Cookies()[0].Value)
}

func TestFrontendExplorerIntegration(t *testing.T) {
	handler, fixture := newExplorerIntegration(t)
	cookie := loginFrontend(t, handler)
	for _, scenario := range []struct {
		path  string
		wants []string
	}{
		{"/databases", []string{"inventaris", "Lihat saja"}},
		{schemaPath, []string{"public", "a%2Fb"}},
		{tablePath, []string{"gen_random_uuid()", "PRIMARY KEY (id)", "primary key"}},
		{databasePath(testDatabaseA, "tables", "public", "a/b"), []string{"public.a/b"}},
		{databasePath(testDatabaseA, "tables", "public", ".."), []string{"public.."}},
		{rowsPath + "?limit=2", []string{"&lt;script&gt;canary-cell&lt;/script&gt;", `class="null"`, "dipotong", "hex", "page=2", "isi halaman bisa bergeser"}},
	} {
		w := dispatchFrontend(handler, "GET", scenario.path, "", cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d", scenario.path, w.Code)
		}
		for _, want := range scenario.wants {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatalf("%s missing %s", scenario.path, want)
			}
		}
		for _, secret := range []string{cookie.Reveal(), testPassword, "database-rahasia", "pg_authid", "<script>canary-cell"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("page leaked hidden or unsafe data")
			}
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("security headers missing")
		}
	}
	if fixture.options.Offset != 0 || fixture.options.FetchLimit != 3 {
		t.Fatal("pagination was not passed to core")
	}
}

func TestFrontendExplorerDenialIntegration(t *testing.T) {
	handler, fixture := newExplorerIntegration(t)
	for _, target := range []string{"/databases", schemaPath, tablePath, rowsPath} {
		w := dispatchFrontend(handler, "GET", target, "", security.Secret{})
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "/login") {
			t.Fatal("anonymous request missing HTML login error")
		}
	}
	cookie := loginFrontend(t, handler)
	for _, target := range []string{rowsPath + "?limit=201", rowsPath + "?page=20002&limit=50", rowsPath + "?page=01", rowsPath + "?page=1&page=2"} {
		w := dispatchFrontend(handler, "GET", target, "", cookie)
		if w.Code != http.StatusBadRequest || fixture.reads != 0 {
			t.Fatal("invalid pagination reached reader")
		}
	}
	fixture.granted = false
	for _, target := range []string{schemaPath, tablePath, rowsPath} {
		w := dispatchFrontend(handler, "GET", target, "", cookie)
		if w.Code != http.StatusForbidden || fixture.reads != 0 {
			t.Fatal("denied request reached reader")
		}
	}
	fixture.store.mu.Lock()
	record := fixture.store.accounts[testUserID]
	record.Account.PlatformAdmin = true
	fixture.store.accounts[testUserID] = record
	fixture.store.mu.Unlock()
	if w := dispatchFrontend(handler, "GET", tablePath, "", cookie); w.Code != http.StatusForbidden {
		t.Fatal("platform admin bypassed database grant")
	}
}

func TestFrontendReadRevocationAndFailuresIntegration(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		failure error
		revoke  bool
		status  int
	}{
		{"driver failure", errors.New("postgres://admin:canary-secret@db"), false, http.StatusServiceUnavailable},
		{"large result", security.ErrResultTooLarge, false, http.StatusUnprocessableEntity},
		{"revoked during read", nil, true, http.StatusUnauthorized},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			handler, fixture := newExplorerIntegration(t)
			cookie := loginFrontend(t, handler)
			fixture.failure = scenario.failure
			if scenario.revoke {
				fixture.onRead = func() {
					fixture.store.mu.Lock()
					defer fixture.store.mu.Unlock()
					record := fixture.store.accounts[testUserID]
					record.Account.AuthVersion++
					fixture.store.accounts[testUserID] = record
				}
			}
			w := dispatchFrontend(handler, "GET", rowsPath, "", cookie)
			if w.Code != scenario.status || strings.Contains(w.Body.String(), "canary-") || strings.Contains(w.Body.String(), "<table") {
				t.Fatal("failed read leaked data or returned wrong status")
			}
		})
	}
}

func TestFrontendMiddlewareErrorsUseHTMLIntegration(t *testing.T) {
	handler, _ := newExplorerIntegration(t)
	r := browserRequest("POST", "/login", loginForm("budi", testPassword), security.Secret{})
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "Muat ulang halaman") {
		t.Fatal("middleware CSRF error did not use the shared HTML renderer")
	}
	for attempt := 0; attempt < 21; attempt++ {
		w = dispatchFrontend(handler, "POST", "/login", "username=budi", security.Secret{})
		want := http.StatusBadRequest
		if attempt == 20 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("middleware form or IP limit error lost status, layout or no-store")
		}
	}
	if w.Header().Get("Retry-After") != "60" {
		t.Fatal("IP limit lost Retry-After header")
	}
}

func TestFrontendUtilityRoutesIntegration(t *testing.T) {
	handler, fixture := newExplorerIntegration(t)
	for _, scenario := range []struct {
		path   string
		status int
	}{
		{"/", http.StatusSeeOther},
		{"/healthz", http.StatusOK},
		{"/static/css/app.css", http.StatusOK},
		{"/static/", http.StatusNotFound},
		{"/missing", http.StatusNotFound},
	} {
		w := dispatchFrontend(handler, "GET", scenario.path, "", security.Secret{})
		if w.Code != scenario.status {
			t.Fatalf("%s returned %d", scenario.path, w.Code)
		}
		if scenario.path == "/" && w.Header().Get("Location") != "/databases" {
			t.Fatal("root did not redirect to databases")
		}
	}
	if fixture.reads != 0 {
		t.Fatal("public routes opened the target database")
	}
}
