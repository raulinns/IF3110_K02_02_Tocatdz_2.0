package ui

import (
	"context"
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

// Pengganti layanan domain dan database target supaya alur HTTP lengkap bisa diuji tanpa PostgreSQL
type memoryExplorer struct {
	mu    sync.Mutex
	calls int
}

func (e *memoryExplorer) count() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
}

func (e *memoryExplorer) Schemas(_ context.Context, request DatabaseRequest) (SchemaOverview, error) {
	e.count()
	return SchemaOverview{DatabaseName: testCatalogNames[request.DatabaseID], Schemas: []SchemaSummary{{Name: "public", Tables: []TableSummary{{Name: "users", Kind: "table"}}}}}, nil
}

func (e *memoryExplorer) Table(_ context.Context, _ DatabaseRequest, schema, table string) (TableDetail, error) {
	e.count()
	if schema != "public" || table != "users" {
		return TableDetail{}, security.ErrNotFound
	}
	return TableDetail{Schema: schema, Name: table, Columns: []ColumnInfo{{Name: "id", Type: "integer"}}}, nil
}

func (e *memoryExplorer) Rows(_ context.Context, _ DatabaseRequest, query RowQuery) (RowPage, error) {
	e.count()
	return RowPage{Columns: []string{"id"}, Rows: [][]Cell{{{Value: "1"}}}}, nil
}

type memoryMigration struct {
	databaseID security.ID
	detail     MigrationDetail
}

type memoryMigrations struct {
	mu      sync.Mutex
	records map[security.ID]memoryMigration
	order   []security.ID
	version int64
	ids     []security.ID
}

func (m *memoryMigrations) find(request DatabaseRequest, id security.ID) (memoryMigration, error) {
	record, ok := m.records[id]
	if !ok || record.databaseID != request.DatabaseID {
		return memoryMigration{}, security.ErrNotFound
	}
	return record, nil
}

func (m *memoryMigrations) History(_ context.Context, request DatabaseRequest) ([]MigrationSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []MigrationSummary
	for _, id := range m.order {
		if record := m.records[id]; record.databaseID == request.DatabaseID {
			result = append(result, record.detail.MigrationSummary)
		}
	}
	return result, nil
}

func (m *memoryMigrations) Migration(_ context.Context, request DatabaseRequest, id security.ID) (MigrationDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.find(request, id)
	return record.detail, err
}

func (m *memoryMigrations) CreateDraft(_ context.Context, request DatabaseRequest, draft MigrationDraft) (security.ID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := []security.ID{testMigrationID, "e5f6a7b8-c9d0-4e1f-8a2b-3c4d5e6f7a8b"}[len(m.order)]
	m.records[id] = memoryMigration{databaseID: request.DatabaseID, detail: MigrationDetail{
		MigrationSummary: MigrationSummary{ID: id, Title: draft.Title, Status: StatusDraft, UpdatedAt: time.Now()},
		SQL:              draft.SQL, Revision: 1,
	}}
	m.order = append(m.order, id)
	return id, nil
}

func (m *memoryMigrations) UpdateDraft(_ context.Context, request DatabaseRequest, id security.ID, draft MigrationDraft, revision uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.find(request, id)
	if err != nil {
		return err
	}
	if record.detail.Status != StatusDraft || record.detail.Revision != revision {
		return security.ErrConflict
	}
	record.detail.Title, record.detail.SQL, record.detail.Revision = draft.Title, draft.SQL, revision+1
	m.records[id] = record
	return nil
}

func (m *memoryMigrations) Execute(_ context.Context, request DatabaseRequest, id security.ID, revision uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.find(request, id)
	if err != nil {
		return err
	}
	if record.detail.Status != StatusDraft || record.detail.Revision != revision {
		return security.ErrConflict
	}
	m.version++
	record.detail.Status, record.detail.Version, record.detail.Actor, record.detail.ExecutedAt = StatusApplied, m.version, "budi", time.Now()
	m.records[id] = record
	return nil
}

type memoryAdmin struct {
	mu     sync.Mutex
	calls  int
	groups []AdminGroup
}

func (a *memoryAdmin) count() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
}

func (a *memoryAdmin) Users(context.Context, security.Principal) ([]AdminUser, error) {
	a.count()
	return []AdminUser{{ID: testUserID, Username: "budi", Active: true}}, nil
}

func (a *memoryAdmin) CreateUser(context.Context, security.Principal, NewUser) error {
	a.count()
	return nil
}

func (a *memoryAdmin) SetUserActive(context.Context, security.Principal, security.ID, bool) error {
	a.count()
	return nil
}

func (a *memoryAdmin) Groups(context.Context, security.Principal) ([]AdminGroup, error) {
	a.count()
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AdminGroup(nil), a.groups...), nil
}

func (a *memoryAdmin) CreateGroup(_ context.Context, _ security.Principal, name string) error {
	a.count()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.groups = append(a.groups, AdminGroup{ID: testGroupID, Name: name})
	return nil
}

func (a *memoryAdmin) AddMember(context.Context, security.Principal, security.ID, string) error {
	a.count()
	return nil
}

func (a *memoryAdmin) RemoveMember(context.Context, security.Principal, security.ID, security.ID) error {
	a.count()
	return nil
}

func (a *memoryAdmin) Connections(context.Context, security.Principal) ([]AdminConnection, error) {
	a.count()
	return nil, nil
}

func (a *memoryAdmin) CreateConnection(context.Context, security.Principal, NewConnection) error {
	a.count()
	return nil
}

func (a *memoryAdmin) Grants(context.Context, security.Principal) ([]AdminGrant, error) {
	a.count()
	return nil, nil
}

func (a *memoryAdmin) SetGrant(context.Context, security.Principal, security.ID, security.ID, security.DatabaseAccess) error {
	a.count()
	return nil
}

func (a *memoryAdmin) RevokeGrant(context.Context, security.Principal, security.ID, security.ID) error {
	a.count()
	return nil
}

var revisionField = regexp.MustCompile(`name="revision" value="(\d+)"`)

func csrfFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	match := csrfField.FindStringSubmatch(w.Body.String())
	if match == nil {
		t.Fatal("page has no CSRF token")
	}
	return match[1]
}

func post(env *integrationEnv, target string, form url.Values, cookie security.Secret) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, browserRequest("POST", target, form.Encode(), cookie))
	return w
}

func TestExplorerPagesIntegration(t *testing.T) {
	env := newIntegrationEnv(t)
	cookie := loginAs(t, env)
	env.store.grant(testDatabaseA, security.AccessViewOnly)
	base := "/databases/" + string(testDatabaseA)
	w := get(env, base+"/schema", cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "inventaris") || !strings.Contains(w.Body.String(), base+"/tables/public/users") {
		t.Fatal("schema page not rendered from explorer")
	}
	if w := get(env, base+"/tables/public/users", cookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "integer") {
		t.Fatal("table page not rendered")
	}
	if w := get(env, base+"/tables/public/missing", cookie); w.Code != http.StatusNotFound {
		t.Fatal("missing table not reported after authorization")
	}
	if w := get(env, base+"/tables/public/users/rows?limit=999", cookie); w.Code != http.StatusBadRequest {
		t.Fatal("unbounded page size accepted")
	}
}

func TestMigrationLifecycleIntegration(t *testing.T) {
	env := newIntegrationEnv(t)
	cookie := loginAs(t, env)
	env.store.grant(testDatabaseB, security.AccessViewEdit)
	base := "/databases/" + string(testDatabaseB) + "/migrations"
	w := get(env, base+"/new", cookie)
	token := csrfFrom(t, w)
	if w := post(env, base, url.Values{"title": {"Buat tabel"}, "sql": {"CREATE TABLE a (id int);"}}, cookie); w.Code != http.StatusForbidden {
		t.Fatal("create without CSRF token accepted")
	}
	w = post(env, base, url.Values{"csrf_token": {token}, "title": {"Buat tabel"}, "sql": {"CREATE TABLE a (id int);"}}, cookie)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != base+"/"+string(testMigrationID) {
		t.Fatal("draft not created")
	}
	detail := base + "/" + string(testMigrationID)
	w = get(env, detail, cookie)
	revision := revisionField.FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || revision == nil || revision[1] != "1" {
		t.Fatal("draft detail missing editor revision")
	}
	update := url.Values{"csrf_token": {token}, "title": {"Buat tabel a"}, "sql": {"CREATE TABLE a (id int primary key);"}, "revision": {"1"}}
	if w := post(env, detail, update, cookie); w.Code != http.StatusSeeOther {
		t.Fatal("draft update failed")
	}
	if w := post(env, detail, update, cookie); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "primary key") {
		t.Fatal("stale update not rejected with input preserved")
	}
	if w := post(env, detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"1"}}, cookie); w.Code != http.StatusConflict {
		t.Fatal("execute with stale revision accepted")
	}
	if w := post(env, detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"2"}}, cookie); w.Code != http.StatusSeeOther {
		t.Fatal("execute failed")
	}
	w = get(env, detail, cookie)
	if !strings.Contains(w.Body.String(), "Berhasil (commit)") || strings.Contains(w.Body.String(), "<textarea") || !strings.Contains(w.Body.String(), "primary key") {
		t.Fatal("executed migration not shown as immutable history")
	}
	w = get(env, base, cookie)
	if !strings.Contains(w.Body.String(), "Buat tabel a") || !strings.Contains(w.Body.String(), "budi") {
		t.Fatal("history missing executed migration")
	}
}

func TestMigrationAccessIntegration(t *testing.T) {
	env := newIntegrationEnv(t)
	cookie := loginAs(t, env)
	env.store.grant(testDatabaseA, security.AccessViewOnly)
	env.store.grant(testDatabaseB, security.AccessViewEdit)
	editorPage := get(env, "/databases/"+string(testDatabaseB)+"/migrations/new", cookie)
	token := csrfFrom(t, editorPage)
	viewOnly := "/databases/" + string(testDatabaseA) + "/migrations"
	if w := get(env, viewOnly, cookie); w.Code != http.StatusOK || strings.Contains(w.Body.String(), viewOnly+"/new") {
		t.Fatal("view only history wrong")
	}
	if w := post(env, viewOnly, url.Values{"csrf_token": {token}, "title": {"x"}, "sql": {"DROP TABLE users;"}}, cookie); w.Code != http.StatusForbidden || len(env.migrations.order) != 0 {
		t.Fatal("view only user created migration")
	}
	created := post(env, "/databases/"+string(testDatabaseB)+"/migrations", url.Values{"csrf_token": {token}, "title": {"x"}, "sql": {"SELECT 1;"}}, cookie)
	if created.Code != http.StatusSeeOther {
		t.Fatal("editor could not create migration")
	}
	// Migration milik database B tidak boleh terbaca lewat URL database A
	if w := get(env, viewOnly+"/"+string(testMigrationID), cookie); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "SELECT 1;") {
		t.Fatal("migration readable through another database")
	}
}

func TestAdminMutationIntegration(t *testing.T) {
	env := newIntegrationEnv(t)
	env.store.mu.Lock()
	record := env.store.accounts[testUserID]
	record.Account.PlatformAdmin = true
	env.store.accounts[testUserID] = record
	env.store.mu.Unlock()
	cookie := loginAs(t, env)
	token := csrfFrom(t, get(env, "/admin/groups", cookie))
	if w := post(env, "/admin/groups", url.Values{"name": {"tim-data"}}, cookie); w.Code != http.StatusForbidden || len(env.admin.groups) != 0 {
		t.Fatal("admin mutation without CSRF accepted")
	}
	if w := post(env, "/admin/groups", url.Values{"csrf_token": {token}, "name": {"tim-data"}}, cookie); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/groups" {
		t.Fatal("group not created")
	}
	if w := get(env, "/admin/groups", cookie); !strings.Contains(w.Body.String(), "tim-data") {
		t.Fatal("created group not listed")
	}
}

func TestMiddlewareRejectionsRenderLayoutErrorPage(t *testing.T) {
	env := newIntegrationEnv(t)
	w := get(env, "/databases", security.Secret{})
	body := w.Body.String()
	if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, "/static/css/app.css") || !strings.Contains(body, `href="/login"`) {
		t.Fatal("expired session not shown as layout error page with login link")
	}
	cookie := loginAs(t, env)
	foreign := browserRequest("POST", "/logout", "", cookie)
	foreign.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	env.mux.ServeHTTP(w, foreign)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Muat ulang halaman") || !strings.Contains(w.Body.String(), "/static/css/app.css") {
		t.Fatal("CSRF rejection not shown as layout error page")
	}
	if w := get(env, "/databases", cookie); w.Code != http.StatusOK {
		t.Fatal("rejected cross-origin logout still ended the session")
	}
}
