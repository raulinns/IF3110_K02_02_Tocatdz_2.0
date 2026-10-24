package ui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/migrationtest"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
)

// Persistence, target SQL, dan admin masih fixture; service migration memakai core F4 asli.
type grantFixture struct {
	store  *memoryStore
	mu     sync.Mutex
	grants map[security.ID][]security.DatabaseAccess
}

func (g *grantFixture) AccessForDatabase(_ context.Context, user, database security.ID) (security.AccessSnapshot, error) {
	g.store.mu.Lock()
	account := g.store.accounts[user].Account
	g.store.mu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	return security.AccessSnapshot{Account: account, GroupAccess: append([]security.DatabaseAccess(nil), g.grants[database]...)}, nil
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

type featureIntegration struct {
	handler    http.Handler
	store      *memoryStore
	grants     *grantFixture
	migrations *migrationtest.Fixture
	admin      *memoryAdmin
}

func newFeatureIntegration(t *testing.T, platformAdmin bool) *featureIntegration {
	t.Helper()
	cfg, store := newIntegrationSecurity(t)
	store.mu.Lock()
	record := store.accounts[testUserID]
	record.Account.PlatformAdmin = platformAdmin
	store.accounts[testUserID] = record
	store.mu.Unlock()
	grants := &grantFixture{store: store, grants: map[security.ID][]security.DatabaseAccess{
		testDatabaseA: {security.AccessViewOnly},
		testDatabaseB: {security.AccessViewOnly, security.AccessViewEdit},
	}}
	authorizer, err := security.NewBackendAuthorizer(store, grants, security.SystemClock{}, security.DefaultSessionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &explorerFixture{store: store, granted: true}
	viewer, err := explorer.NewService(explorer.Config{Directory: catalog, Reader: catalog, Authorizer: authorizer})
	if err != nil {
		t.Fatal(err)
	}
	env := &featureIntegration{store: store, grants: grants, migrations: migrationtest.New(), admin: &memoryAdmin{}}
	env.migrations.Check = func(ctx context.Context, req migration.Request) error {
		return authorizer.RequireDatabase(ctx, req.Principal, req.DatabaseID, security.ActionExecuteMigration)
	}
	migrations, err := migration.NewService(migration.Config{
		Repository: env.migrations, Coordinator: env.migrations, Validator: migration.NewPostgreSQLValidator(),
		Authorizer: authorizer, Accounts: store, IDs: security.NewTokenGenerator(), Clock: security.SystemClock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := NewFrontend(FrontendConfig{
		Security: cfg, Viewer: viewer, Authorizer: authorizer, Migrations: migrations,
		Admin: &AdminServices{Users: env.admin, Groups: env.admin, Connections: env.admin, Grants: env.admin},
	})
	if err != nil {
		t.Fatal(err)
	}
	env.handler = web.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), frontend)
	return env
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

func (e *featureIntegration) get(target string, cookie security.Secret) *httptest.ResponseRecorder {
	return dispatchFrontend(e.handler, "GET", target, "", cookie)
}

func (e *featureIntegration) post(target string, form url.Values, cookie security.Secret) *httptest.ResponseRecorder {
	return dispatchFrontend(e.handler, "POST", target, form.Encode(), cookie)
}

func TestFrontendMigrationLifecycleIntegration(t *testing.T) {
	env := newFeatureIntegration(t, false)
	cookie := loginFrontend(t, env.handler)
	base := "/databases/" + string(testDatabaseB) + "/migrations"
	token := csrfFrom(t, env.get(base+"/new", cookie))
	if w := env.post(base, url.Values{"title": {"Buat tabel"}, "sql": {"CREATE TABLE a (id int);"}}, cookie); w.Code != http.StatusForbidden || len(env.migrations.Records()) != 0 {
		t.Fatal("create without CSRF token accepted")
	}
	w := env.post(base, url.Values{"csrf_token": {token}, "title": {"Buat tabel"}, "sql": {"CREATE TABLE a (id int);"}}, cookie)
	detail := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(detail, base+"/") || !security.ValidID(security.ID(strings.TrimPrefix(detail, base+"/"))) {
		t.Fatal("draft not created")
	}
	w = env.get(detail, cookie)
	if revision := revisionField.FindStringSubmatch(w.Body.String()); w.Code != http.StatusOK || revision == nil || revision[1] != "1" {
		t.Fatal("draft detail missing editor revision")
	}
	update := url.Values{"csrf_token": {token}, "title": {"Buat tabel a"}, "sql": {"CREATE TABLE a (id int primary key);"}, "revision": {"1"}}
	if w := env.post(detail, update, cookie); w.Code != http.StatusSeeOther {
		t.Fatal("draft update failed")
	}
	if w := env.post(detail, update, cookie); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "primary key") {
		t.Fatal("stale update not rejected with input preserved")
	}
	if w := env.post(detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"1"}}, cookie); w.Code != http.StatusConflict {
		t.Fatal("execute with stale revision accepted")
	}
	if w := env.post(detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"2"}}, cookie); w.Code != http.StatusSeeOther {
		t.Fatal("execute failed")
	}
	w = env.get(detail, cookie)
	if !strings.Contains(w.Body.String(), "Berhasil (commit)") || strings.Contains(w.Body.String(), "<textarea") || !strings.Contains(w.Body.String(), "primary key") {
		t.Fatal("executed migration not shown as immutable history")
	}
	if w := env.get(base, cookie); !strings.Contains(w.Body.String(), "Buat tabel a") || !strings.Contains(w.Body.String(), "budi") {
		t.Fatal("history missing executed migration")
	}
}

func TestFrontendMigrationAccessIntegration(t *testing.T) {
	env := newFeatureIntegration(t, false)
	cookie := loginFrontend(t, env.handler)
	token := csrfFrom(t, env.get("/databases/"+string(testDatabaseB)+"/migrations/new", cookie))
	viewOnly := "/databases/" + string(testDatabaseA) + "/migrations"
	if w := env.get(viewOnly, cookie); w.Code != http.StatusOK || strings.Contains(w.Body.String(), viewOnly+"/new") {
		t.Fatal("view only history wrong")
	}
	if w := env.get(viewOnly+"/new", cookie); w.Code != http.StatusForbidden {
		t.Fatal("view only user opened editor")
	}
	if w := env.post(viewOnly, url.Values{"csrf_token": {token}, "title": {"x"}, "sql": {"DROP TABLE users;"}}, cookie); w.Code != http.StatusForbidden || len(env.migrations.Records()) != 0 {
		t.Fatal("view only user created migration")
	}
	created := env.post("/databases/"+string(testDatabaseB)+"/migrations", url.Values{"csrf_token": {token}, "title": {"x"}, "sql": {"SELECT 1;"}}, cookie)
	if created.Code != http.StatusSeeOther {
		t.Fatal("editor could not create migration")
	}
	// Migration milik database B tidak boleh terbaca lewat URL database A
	records := env.migrations.Records()
	if len(records) != 1 {
		t.Fatal("draft missing from ledger")
	}
	if w := env.get(viewOnly+"/"+string(records[0].ID), cookie); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "SELECT 1;") {
		t.Fatal("migration readable through another database")
	}
	if w := env.get("/databases/"+string(testDatabaseC)+"/migrations", cookie); w.Code != http.StatusForbidden {
		t.Fatal("history readable without grant")
	}
}

func TestFrontendAdminIntegration(t *testing.T) {
	user := newFeatureIntegration(t, false)
	cookie := loginFrontend(t, user.handler)
	if w := user.get("/admin/users", cookie); w.Code != http.StatusForbidden || user.admin.calls != 0 {
		t.Fatal("ordinary user reached admin service")
	}
	if w := user.get("/databases", cookie); strings.Contains(w.Body.String(), `href="/admin/users"`) {
		t.Fatal("ordinary user offered admin link")
	}
	admin := newFeatureIntegration(t, true)
	cookie = loginFrontend(t, admin.handler)
	w := admin.get("/admin/groups", cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `href="/admin/users"`) {
		t.Fatal("platform admin cannot open admin pages")
	}
	token := csrfFrom(t, w)
	if w := admin.post("/admin/groups", url.Values{"name": {"tim-data"}}, cookie); w.Code != http.StatusForbidden || len(admin.admin.groups) != 0 {
		t.Fatal("admin mutation without CSRF accepted")
	}
	if w := admin.post("/admin/groups", url.Values{"csrf_token": {token}, "name": {"tim-data"}}, cookie); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/groups" {
		t.Fatal("group not created")
	}
	if w := admin.get("/admin/groups", cookie); !strings.Contains(w.Body.String(), "tim-data") {
		t.Fatal("created group not listed")
	}
	if w := admin.post("/admin/users", url.Values{"csrf_token": {token}, "username": {"siti"}, "password": {"pendek"}}, cookie); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Password minimal 15 karakter") {
		t.Fatal("password policy reason not shown")
	}
}

func TestFrontendHidesFeaturesWithoutServices(t *testing.T) {
	handler, _ := newExplorerIntegration(t)
	cookie := loginFrontend(t, handler)
	for _, target := range []string{"/databases", schemaPath} {
		if w := dispatchFrontend(handler, "GET", target, "", cookie); strings.Contains(w.Body.String(), "/migrations") || strings.Contains(w.Body.String(), "/admin/") {
			t.Fatal("link to unregistered feature shown: " + target)
		}
	}
	for _, target := range []string{"/databases/" + string(testDatabaseA) + "/migrations", "/admin/users"} {
		if w := dispatchFrontend(handler, "GET", target, "", cookie); w.Code != http.StatusNotFound {
			t.Fatal("unconfigured feature route registered: " + target)
		}
	}
}
