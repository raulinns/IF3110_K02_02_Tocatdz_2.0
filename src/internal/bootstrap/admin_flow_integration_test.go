package bootstrap

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestAdminWorkflowIntegratesUIAndPostgreSQL(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()
	cookie := e.login(t, "admin", adminPassword)
	token := match(t, csrfField, e.do("GET", "/admin/users", nil, cookie).Body.String())
	post := func(path string, form url.Values, want int) {
		t.Helper()
		form.Set("csrf_token", token)
		w := e.do("POST", path, form, cookie)
		if w.Code != want {
			t.Fatalf("%s: got %d, want %d", path, w.Code, want)
		}
	}
	post("/admin/users", url.Values{"username": {"tester"}, "password": {"password tester cukup panjang"}}, http.StatusSeeOther)
	post("/admin/users", url.Values{"username": {"tester"}, "password": {"password tester cukup panjang"}}, http.StatusConflict)
	post("/admin/groups", url.Values{"name": {"qa"}}, http.StatusSeeOther)
	var user, group, database string
	for query, out := range map[string]*string{"SELECT id::text FROM users WHERE username='tester'": &user, "SELECT id::text FROM groups WHERE name='qa'": &group, "SELECT id::text FROM managed_databases WHERE name='demo'": &database} {
		if err := e.platform.pool.QueryRow(ctx, query).Scan(out); err != nil {
			t.Fatal(err)
		}
	}
	member := "/admin/groups/" + group + "/members"
	post(member, url.Values{"username": {"tester"}}, http.StatusSeeOther)
	post("/admin/grants", url.Values{"group_id": {group}, "database_id": {database}, "access": {"view_edit"}}, http.StatusSeeOther)
	userCookie := e.login(t, "tester", "password tester cukup panjang")
	base := "/databases/" + database
	if w := e.do("GET", base+"/migrations/new", nil, userCookie); w.Code != http.StatusOK {
		t.Fatal("new group member cannot edit")
	}
	post("/admin/grants", url.Values{"group_id": {group}, "database_id": {database}, "access": {"view_only"}}, http.StatusSeeOther)
	if w := e.do("GET", base+"/schema", nil, userCookie); w.Code != http.StatusUnauthorized {
		t.Fatal("downgrade did not revoke existing session")
	}
	userCookie = e.login(t, "tester", "password tester cukup panjang")
	if w := e.do("GET", base+"/schema", nil, userCookie); w.Code != http.StatusOK {
		t.Fatal("downgraded user cannot view")
	}
	if w := e.do("GET", base+"/migrations/new", nil, userCookie); w.Code != http.StatusForbidden {
		t.Fatal("downgraded user can edit")
	}
	post(member+"/"+user+"/remove", url.Values{}, http.StatusSeeOther)
	if w := e.do("GET", "/databases", nil, userCookie); w.Code != http.StatusUnauthorized {
		t.Fatal("removal did not revoke session")
	}
	userCookie = e.login(t, "tester", "password tester cukup panjang")
	if w := e.do("GET", base+"/schema", nil, userCookie); w.Code != http.StatusForbidden {
		t.Fatal("user without grant accessed database")
	}
	post("/admin/users/"+user+"/active", url.Values{"active": {"false"}}, http.StatusSeeOther)
	if w := e.do("GET", "/databases", nil, userCookie); w.Code != http.StatusUnauthorized {
		t.Fatal("inactive user retained session")
	}
	if w := e.do("POST", "/login", url.Values{"username": {"tester"}, "password": {"password tester cukup panjang"}}, ""); w.Code != http.StatusUnauthorized {
		t.Fatal("inactive user logged in")
	}
	post("/admin/users/"+user+"/active", url.Values{"active": {"true"}}, http.StatusSeeOther)
	e.login(t, "tester", "password tester cukup panjang")
	post("/admin/grants/revoke", url.Values{"group_id": {group}, "database_id": {database}}, http.StatusSeeOther)
	post("/admin/connections", url.Values{"name": {"unreachable"}, "dsn": {"postgres://nobody:canary-password@127.0.0.1:1/none?connect_timeout=1"}}, http.StatusBadRequest)
	box, _ := postgres.NewSecretBox(key(3))
	adminDSN := security.NewSecret(createDatabase(t))
	post("/admin/connections", url.Values{"name": {"unsafe"}, "dsn": {adminDSN.Reveal()}}, http.StatusBadRequest)
	dsn, err := postgres.PrepareManagedTarget(ctx, e.platform.pool, adminDSN, box)
	if err != nil {
		t.Fatal(err)
	}
	post("/admin/connections", url.Values{"name": {"second-target"}, "dsn": {dsn.Reveal()}}, http.StatusSeeOther)
	w := e.do("GET", "/admin/connections", nil, cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "second-target") || strings.Contains(w.Body.String(), dsn.Reveal()) || strings.Contains(w.Body.String(), "canary-password") {
		t.Fatal("connection UI missing data or disclosing credentials")
	}
	w = e.do("GET", "/databases", nil, cookie)
	if strings.Contains(w.Body.String(), "second-target") {
		t.Fatal("platform admin bypassed group grant")
	}
}
