package ui

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/migrationtest"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
)

type migrationEnv struct {
	handler http.Handler
	ledger  *migrationtest.Fixture
	access  *explorerFixture
	logs    *bytes.Buffer
}

func newMigrationEnv(t *testing.T) migrationEnv {
	t.Helper()
	cfg, store := newIntegrationSecurity(t)
	access := &explorerFixture{store: store, granted: true, access: security.AccessViewEdit}
	authorizer, err := security.NewBackendAuthorizer(store, access, security.SystemClock{}, security.DefaultSessionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := explorer.NewService(explorer.Config{Directory: access, Reader: access, Authorizer: authorizer})
	if err != nil {
		t.Fatal(err)
	}
	ledger := migrationtest.New()
	ledger.Check = func(ctx context.Context, req migration.Request) error {
		return authorizer.RequireDatabase(ctx, req.Principal, req.DatabaseID, security.ActionExecuteMigration)
	}
	service, err := migration.NewService(migration.Config{Repository: ledger, Coordinator: ledger, Validator: migration.NewPostgreSQLValidator(), Authorizer: authorizer, Accounts: store, IDs: security.NewTokenGenerator(), Clock: security.SystemClock{}})
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := NewFrontend(FrontendConfig{Security: cfg, Viewer: viewer, Authorizer: authorizer, Migrations: service})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	return migrationEnv{handler: web.NewRouter(slog.New(slog.NewTextHandler(logs, nil)), frontend), ledger: ledger, access: access, logs: logs}
}
func migrationToken(t *testing.T, env migrationEnv, cookie security.Secret) string {
	t.Helper()
	w := dispatchFrontend(env.handler, "GET", migrationsPath+"/new", "", cookie)
	match := csrfField.FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || match == nil {
		t.Fatal("editor missing CSRF token")
	}
	return match[1]
}
func createMigration(t *testing.T, env migrationEnv, cookie security.Secret, token, sql string) string {
	t.Helper()
	w := dispatchFrontend(env.handler, "POST", migrationsPath, url.Values{"csrf_token": {token}, "title": {"Buat tabel"}, "sql": {sql}}.Encode(), cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create returned %d", w.Code)
	}
	return w.Header().Get("Location")
}
func TestMigrationHTTPWorkflowIntegration(t *testing.T) {
	env := newMigrationEnv(t)
	cookie := loginFrontend(t, env.handler)
	token := migrationToken(t, env, cookie)
	const sql = "CREATE TABLE public.items (id int PRIMARY KEY, note text DEFAULT 'SQL_PRIVATE_CANARY');"
	detail := createMigration(t, env, cookie, token, sql)
	w := dispatchFrontend(env.handler, "POST", detail, url.Values{"csrf_token": {token}, "title": {"Buat tabel revisi"}, "sql": {sql}, "revision": {"1"}}.Encode(), cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatal("draft update failed")
	}
	w = dispatchFrontend(env.handler, "POST", detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"1"}}.Encode(), cookie)
	if w.Code != http.StatusConflict || env.ledger.Snapshot().Executes != 0 {
		t.Fatal("stale execution accepted")
	}
	w = dispatchFrontend(env.handler, "POST", detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"2"}}.Encode(), cookie)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != detail {
		t.Fatal("execution did not redirect to result")
	}
	w = dispatchFrontend(env.handler, "GET", detail, "", cookie)
	for _, want := range []string{"Berhasil (commit)", "budi", "SQL_PRIVATE_CANARY"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal("detail missing execution metadata")
		}
	}
	if strings.Contains(w.Body.String(), "<textarea") || strings.Contains(w.Body.String(), `action="`+detail+`/execute"`) {
		t.Fatal("executed migration remained editable")
	}
	for range 2 {
		createMigration(t, env, cookie, token, "SELECT 1;")
	}
	w = dispatchFrontend(env.handler, "GET", migrationsPath+"?limit=2", "", cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "page=2") {
		t.Fatal("history pagination not connected")
	}
	for _, secret := range []string{sql, "SQL_PRIVATE_CANARY", token, cookie.Reveal(), testPassword} {
		if strings.Contains(env.logs.String(), secret) {
			t.Fatal("HTTP logs leaked SQL or credentials")
		}
	}
}
func TestMigrationHTTPFailuresIntegration(t *testing.T) {
	for _, scenario := range []struct {
		name, sql string
		configure func(*migrationtest.Fixture)
		want      int
		message   string
	}{
		{"SQL failure", "CREATE TABLE public.t(id int);", func(f *migrationtest.Fixture) { f.SQLError = errors.New("driver-canary-secret") }, http.StatusSeeOther, "Gagal (rollback)"},
		{"commit uncertain", "CREATE TABLE public.t(id int);", func(f *migrationtest.Fixture) { f.CommitError = security.ErrUnavailable }, http.StatusServiceUnavailable, "Hasil migration belum pasti"},
		{"SQL rejected", "BEGIN; SELECT 1; COMMIT;", func(*migrationtest.Fixture) {}, http.StatusBadRequest, "Perintah transaksi diatur oleh aplikasi"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			env := newMigrationEnv(t)
			cookie := loginFrontend(t, env.handler)
			token := migrationToken(t, env, cookie)
			detail := createMigration(t, env, cookie, token, scenario.sql)
			scenario.configure(env.ledger)
			w := dispatchFrontend(env.handler, "POST", detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"1"}}.Encode(), cookie)
			if w.Code != scenario.want {
				t.Fatalf("got %d want %d", w.Code, scenario.want)
			}
			if w.Code == http.StatusSeeOther {
				w = dispatchFrontend(env.handler, "GET", detail, "", cookie)
			}
			if !strings.Contains(w.Body.String(), scenario.message) || strings.Contains(w.Body.String(), "driver-canary-secret") || strings.Contains(env.logs.String(), "driver-canary-secret") {
				t.Fatal("unsafe or incorrect failure response")
			}
			if scenario.want == http.StatusServiceUnavailable {
				w = dispatchFrontend(env.handler, "GET", detail, "", cookie)
				if !strings.Contains(w.Body.String(), "Jangan jalankan ulang") || strings.Contains(w.Body.String(), "<textarea") {
					t.Fatal("pending allows editing or misses warning")
				}
			}
		})
	}
}
func TestMigrationHTTPAuthorizationAndCSRFIntegration(t *testing.T) {
	env := newMigrationEnv(t)
	cookie := loginFrontend(t, env.handler)
	token := migrationToken(t, env, cookie)
	detail := createMigration(t, env, cookie, token, "SELECT 1;")
	w := dispatchFrontend(env.handler, "POST", detail+"/execute", url.Values{"revision": {"1"}}.Encode(), cookie)
	if w.Code != http.StatusForbidden || env.ledger.Snapshot().Begins != 0 {
		t.Fatal("execution without CSRF accepted")
	}
	for _, target := range []string{migrationsPath, detail} {
		if w := dispatchFrontend(env.handler, "GET", target, "", security.Secret{}); w.Code != http.StatusUnauthorized {
			t.Fatal("anonymous migration visible")
		}
	}
	env.access.access = security.AccessViewOnly
	if w := dispatchFrontend(env.handler, "GET", detail, "", cookie); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<textarea") {
		t.Fatal("viewer detail not read-only")
	}
	if w := dispatchFrontend(env.handler, "POST", detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"1"}}.Encode(), cookie); w.Code != http.StatusForbidden || env.ledger.Snapshot().Begins != 0 {
		t.Fatal("viewer executed migration")
	}
	env.access.access = security.AccessViewEdit
	env.ledger.AfterLock = func() {
		env.access.store.mu.Lock()
		defer env.access.store.mu.Unlock()
		record := env.access.store.accounts[testUserID]
		record.Account.AuthVersion++
		env.access.store.accounts[testUserID] = record
	}
	if w := dispatchFrontend(env.handler, "POST", detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"1"}}.Encode(), cookie); w.Code != http.StatusUnauthorized || env.ledger.Snapshot().Begins != 0 {
		t.Fatal("revoked session dispatched SQL")
	}
}
func TestMigrationLargeEncodedFormIntegration(t *testing.T) {
	env := newMigrationEnv(t)
	cookie := loginFrontend(t, env.handler)
	token := migrationToken(t, env, cookie)
	sql := "SELECT 1; /*" + strings.Repeat("%", migration.MaxSQLBytes-len("SELECT 1; /**/")) + "*/"
	if len(sql) != migration.MaxSQLBytes {
		t.Fatal("bad boundary fixture")
	}
	form := url.Values{"csrf_token": {token}, "title": {"Large"}, "sql": {sql}}.Encode()
	if len(form) <= 1<<20 {
		t.Fatal("form is not testing encoded expansion")
	}
	w := dispatchFrontend(env.handler, "POST", migrationsPath, form, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("valid expanded form returned %d", w.Code)
	}
	w = dispatchFrontend(env.handler, "POST", migrationsPath, url.Values{"csrf_token": {token}, "title": {"Too large"}, "sql": {sql + "x"}}.Encode(), cookie)
	if w.Code != http.StatusBadRequest {
		t.Fatal("oversized decoded SQL accepted")
	}
	if len(env.ledger.Records()) != 1 {
		t.Fatal("invalid draft persisted")
	}
}

func TestMigrationHistoryQueryRejectedBeforeService(t *testing.T) {
	for _, query := range []string{"?page=0", "?limit=201", "?page=01", "?page=1&page=2", "?unknown=1", "?page=" + strconv.Itoa(20002) + "&limit=50"} {
		service := &fakeMigrations{}
		w := serve(testMigrationUI(t, service, &fakeAuthorizer{}), protectedRequest("GET", migrationsPath+query, nil))
		if w.Code != http.StatusBadRequest || service.calls != 0 {
			t.Fatal("invalid history query reached service")
		}
	}
}
