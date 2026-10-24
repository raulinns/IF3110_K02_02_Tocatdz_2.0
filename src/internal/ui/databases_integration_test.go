package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var testCatalogNames = map[security.ID]string{
	testDatabaseA: "inventaris",
	testDatabaseB: "keuangan",
	testDatabaseC: "arsip",
}

// Katalog uji: semua database ada, tapi hanya yang punya grant yang boleh keluar
type memoryCatalog struct{ store *memoryStore }

func (c *memoryCatalog) ListAccessible(ctx context.Context, principal security.Principal) ([]DatabaseSummary, error) {
	var result []DatabaseSummary
	for _, id := range []security.ID{testDatabaseA, testDatabaseB, testDatabaseC} {
		snapshot, err := c.store.AccessForDatabase(ctx, principal.UserID, id)
		if err != nil {
			return nil, err
		}
		access, err := security.EffectiveAccess(snapshot.GroupAccess)
		if err != nil {
			return nil, security.ErrUnavailable
		}
		if access != security.AccessNone {
			result = append(result, DatabaseSummary{ID: id, Name: testCatalogNames[id], Access: access})
		}
	}
	return result, nil
}

func loginAs(t *testing.T, env *integrationEnv) security.Secret {
	t.Helper()
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, browserRequest("POST", "/login", loginForm("budi", testPassword), security.Secret{}))
	if w.Code != http.StatusSeeOther || len(w.Result().Cookies()) != 1 {
		t.Fatal("login failed")
	}
	return security.NewSecret(w.Result().Cookies()[0].Value)
}

func get(env *integrationEnv, target string, cookie security.Secret) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, browserRequest("GET", target, "", cookie))
	return w
}

func TestDatabaseListShowsOnlyGrantedDatabases(t *testing.T) {
	env := newIntegrationEnv(t)
	cookie := loginAs(t, env)
	w := get(env, "/databases", cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "belum memiliki akses") {
		t.Fatal("user without grants did not get empty list")
	}
	env.store.grant(testDatabaseA, security.AccessViewOnly)
	env.store.grant(testDatabaseB, security.AccessViewOnly, security.AccessViewEdit)
	w = get(env, "/databases", cookie)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "inventaris") || !strings.Contains(body, "keuangan") || strings.Contains(body, "arsip") {
		t.Fatal("list does not match grants")
	}
	if !strings.Contains(body, "Lihat saja") || !strings.Contains(body, "Lihat &#43; ubah") || !strings.Contains(body, "budi") {
		t.Fatal("access labels or session identity missing")
	}
	if strings.Contains(body, cookie.Reveal()) {
		t.Fatal("session cookie leaked into page")
	}
}

func TestDatabaseListRequiresLogin(t *testing.T) {
	env := newIntegrationEnv(t)
	env.store.grant(testDatabaseA, security.AccessViewEdit)
	w := get(env, "/databases", security.Secret{})
	if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "inventaris") {
		t.Fatal("database list served without session")
	}
}

func TestDatabaseRoutesEnforceGrantsPerDatabaseAndAction(t *testing.T) {
	env := newIntegrationEnv(t)
	cookie := loginAs(t, env)
	env.store.grant(testDatabaseA, security.AccessViewOnly)
	env.store.grant(testDatabaseB, security.AccessViewEdit)
	for _, scenario := range []struct {
		name   string
		target string
		want   int
		loads  int
	}{
		{"view only can view schema", "/databases/" + string(testDatabaseA) + "/schema", http.StatusOK, 1},
		{"view only can view rows", "/databases/" + string(testDatabaseA) + "/tables/public/users/rows", http.StatusOK, 1},
		{"view only cannot open editor", "/databases/" + string(testDatabaseA) + "/migrations/new", http.StatusForbidden, 0},
		{"view edit can view schema", "/databases/" + string(testDatabaseB) + "/schema", http.StatusOK, 1},
		{"view edit can open editor", "/databases/" + string(testDatabaseB) + "/migrations/new", http.StatusOK, 0},
		{"no grant cannot view schema", "/databases/" + string(testDatabaseC) + "/schema", http.StatusForbidden, 0},
		{"no grant cannot view rows", "/databases/" + string(testDatabaseC) + "/tables/public/users/rows", http.StatusForbidden, 0},
		{"unknown database looks like no grant", "/databases/0b8e5a40-1c2d-4e3f-8a9b-7c6d5e4f3a2b/schema", http.StatusForbidden, 0},
		{"malformed id is rejected", "/databases/bukan-uuid/schema", http.StatusBadRequest, 0},
	} {
		before := env.explorer.calls
		w := get(env, scenario.target, cookie)
		if w.Code != scenario.want || env.explorer.calls-before != scenario.loads {
			t.Fatal("wrong access decision: " + scenario.name)
		}
		if scenario.want == http.StatusForbidden && !strings.Contains(w.Body.String(), "Akses ditolak") {
			t.Fatal("denial page missing: " + scenario.name)
		}
	}
}

func TestAdminRouteRequiresPlatformAdmin(t *testing.T) {
	env := newIntegrationEnv(t)
	cookie := loginAs(t, env)
	if w := get(env, "/admin/users", cookie); w.Code != http.StatusForbidden || env.admin.calls != 0 {
		t.Fatal("ordinary user reached admin route")
	}
	if w := get(env, "/databases", cookie); strings.Contains(w.Body.String(), `href="/admin/users"`) {
		t.Fatal("ordinary user offered admin link")
	}
	env.store.mu.Lock()
	record := env.store.accounts[testUserID]
	record.Account.PlatformAdmin = true
	env.store.accounts[testUserID] = record
	env.store.mu.Unlock()
	if w := get(env, "/admin/users", cookie); w.Code != http.StatusOK || env.admin.calls != 1 || !strings.Contains(w.Body.String(), `href="/admin/users"`) {
		t.Fatal("platform admin rejected")
	}
}

func TestRevokedSessionLosesDatabaseAccess(t *testing.T) {
	env := newIntegrationEnv(t)
	cookie := loginAs(t, env)
	env.store.grant(testDatabaseA, security.AccessViewOnly)
	if w := get(env, "/databases/"+string(testDatabaseA)+"/schema", cookie); w.Code != http.StatusOK {
		t.Fatal("granted access rejected")
	}
	env.store.mu.Lock()
	for id, session := range env.store.sessions {
		session.RevokedAt = &session.CreatedAt
		env.store.sessions[id] = session
	}
	env.store.mu.Unlock()
	before := env.explorer.calls
	if w := get(env, "/databases/"+string(testDatabaseA)+"/schema", cookie); w.Code != http.StatusUnauthorized || env.explorer.calls != before {
		t.Fatal("revoked session still reached handler")
	}
}
