package ui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const testGroupID security.ID = "d4e5f6a7-b8c9-4d0e-8f1a-2b3c4d5e6f70"

type fakeAdmin struct {
	calls     []string
	principal security.Principal
	newUser   NewUser
	userID    security.ID
	active    bool
	name      string
	groupID   security.ID
	username  string
	conn      NewConnection
	database  security.ID
	access    security.DatabaseAccess
	err       error
}

func (a *fakeAdmin) record(call string, principal security.Principal) error {
	a.calls = append(a.calls, call)
	a.principal = principal
	return a.err
}

func (a *fakeAdmin) Users(_ context.Context, p security.Principal) ([]AdminUser, error) {
	return []AdminUser{{ID: testUserID, Username: "budi", Active: true}}, a.record("users", p)
}

func (a *fakeAdmin) CreateUser(_ context.Context, p security.Principal, user NewUser) error {
	a.newUser = user
	return a.record("create-user", p)
}

func (a *fakeAdmin) SetUserActive(_ context.Context, p security.Principal, id security.ID, active bool) error {
	a.userID, a.active = id, active
	return a.record("set-active", p)
}

func (a *fakeAdmin) Groups(_ context.Context, p security.Principal) ([]AdminGroup, error) {
	return []AdminGroup{{ID: testGroupID, Name: "tim-data", Members: []AdminUser{{ID: testUserID, Username: "budi"}}}}, a.record("groups", p)
}

func (a *fakeAdmin) CreateGroup(_ context.Context, p security.Principal, name string) error {
	a.name = name
	return a.record("create-group", p)
}

func (a *fakeAdmin) AddMember(_ context.Context, p security.Principal, groupID security.ID, username string) error {
	a.groupID, a.username = groupID, username
	return a.record("add-member", p)
}

func (a *fakeAdmin) RemoveMember(_ context.Context, p security.Principal, groupID, userID security.ID) error {
	a.groupID, a.userID = groupID, userID
	return a.record("remove-member", p)
}

func (a *fakeAdmin) Connections(_ context.Context, p security.Principal) ([]AdminConnection, error) {
	return []AdminConnection{{ID: testDatabaseA, Name: "inventaris", Engine: dbms.MySQL}}, a.record("connections", p)
}

func (a *fakeAdmin) CreateConnection(_ context.Context, p security.Principal, connection NewConnection) error {
	a.conn = connection
	return a.record("create-connection", p)
}

func (a *fakeAdmin) Grants(_ context.Context, p security.Principal) ([]AdminGrant, error) {
	return []AdminGrant{{GroupID: testGroupID, GroupName: "tim-data", DatabaseID: testDatabaseA, DatabaseName: "inventaris", Access: security.AccessViewEdit}}, a.record("grants", p)
}

func (a *fakeAdmin) SetGrant(_ context.Context, p security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) error {
	a.groupID, a.database, a.access = groupID, databaseID, access
	return a.record("set-grant", p)
}

func (a *fakeAdmin) RevokeGrant(_ context.Context, p security.Principal, groupID, databaseID security.ID) error {
	a.groupID, a.database = groupID, databaseID
	return a.record("revoke-grant", p)
}

func adminServices(admin *fakeAdmin) AdminServices {
	return AdminServices{Users: admin, Groups: admin, Connections: admin, Grants: admin}
}

func testAdminUI(t *testing.T, admin *fakeAdmin, authorizer *fakeAuthorizer) *http.ServeMux {
	t.Helper()
	renderer, _ := NewRenderer()
	access, _ := NewDatabaseGuard(authorizer)
	ui, err := NewAdminUI(adminServices(admin), &fakeGuard{}, access, renderer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ui.RegisterRoutes(mux)
	return mux
}

var memberPath = "/admin/groups/" + string(testGroupID) + "/members"

func adminRoutes() []struct {
	method string
	target string
	form   url.Values
} {
	return []struct {
		method string
		target string
		form   url.Values
	}{
		{"GET", "/admin", nil},
		{"GET", "/admin/users", nil},
		{"POST", "/admin/users", url.Values{"username": {"siti"}, "password": {testPassword}}},
		{"POST", "/admin/users/" + string(testUserID) + "/active", url.Values{"active": {"false"}}},
		{"GET", "/admin/groups", nil},
		{"POST", "/admin/groups", url.Values{"name": {"tim"}}},
		{"POST", memberPath, url.Values{"username": {"siti"}}},
		{"POST", memberPath + "/" + string(testUserID) + "/remove", url.Values{}},
		{"GET", "/admin/connections", nil},
		{"POST", "/admin/connections", url.Values{"engine": {"postgresql"}, "name": {"x"}, "dsn": {"postgres://u:p@h/db"}}},
		{"GET", "/admin/grants", nil},
		{"POST", "/admin/grants", url.Values{"group_id": {string(testGroupID)}, "database_id": {string(testDatabaseA)}, "access": {"view_only"}}},
		{"POST", "/admin/grants/revoke", url.Values{"group_id": {string(testGroupID)}, "database_id": {string(testDatabaseA)}}},
	}
}

func TestNewAdminUIRejectsMissingDependencies(t *testing.T) {
	renderer, _ := NewRenderer()
	access, _ := NewDatabaseGuard(&fakeAuthorizer{})
	admin := &fakeAdmin{}
	partial := adminServices(admin)
	partial.Grants = nil
	for _, build := range []func() (*AdminUI, error){
		func() (*AdminUI, error) { return NewAdminUI(partial, &fakeGuard{}, access, renderer) },
		func() (*AdminUI, error) { return NewAdminUI(adminServices(admin), nil, access, renderer) },
		func() (*AdminUI, error) { return NewAdminUI(adminServices(admin), &fakeGuard{}, nil, renderer) },
		func() (*AdminUI, error) { return NewAdminUI(adminServices(admin), &fakeGuard{}, access, nil) },
	} {
		if ui, err := build(); ui != nil || !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing dependency accepted")
		}
	}
}

func TestAdminRoutesRejectNonAdminWithoutSideEffects(t *testing.T) {
	for _, route := range adminRoutes() {
		admin := &fakeAdmin{}
		w := serve(testAdminUI(t, admin, &fakeAuthorizer{adminErr: security.ErrForbidden}), formRequest(route.method, route.target, route.form))
		if w.Code != http.StatusForbidden || len(admin.calls) != 0 {
			t.Fatal("non-admin reached admin service: " + route.method + " " + route.target)
		}
	}
}

func TestAdminRoutesSucceedForAdmin(t *testing.T) {
	for _, route := range adminRoutes() {
		admin := &fakeAdmin{}
		authorizer := &fakeAuthorizer{}
		w := serve(testAdminUI(t, admin, authorizer), formRequest(route.method, route.target, route.form))
		want := http.StatusOK
		if route.method == "POST" || route.target == "/admin" {
			want = http.StatusSeeOther
		}
		if w.Code != want || authorizer.adminCalls != 1 {
			t.Fatal("admin route failed: " + route.method + " " + route.target)
		}
		if route.target != "/admin" && admin.principal != testPrincipal {
			t.Fatal("admin service did not receive principal: " + route.target)
		}
	}
}

func TestAdminPagesRenderData(t *testing.T) {
	for _, scenario := range []struct {
		target string
		want   []string
	}{
		{"/admin/users", []string{"budi", "Nonaktifkan", "/admin/users/" + string(testUserID) + "/active"}},
		{"/admin/groups", []string{"tim-data", "budi", memberPath + "/" + string(testUserID) + "/remove"}},
		{"/admin/connections", []string{"inventaris", "MySQL", "tidak akan ditampilkan lagi"}},
		{"/admin/grants", []string{"tim-data", "inventaris", "Lihat &#43; ubah", `value="` + string(testGroupID) + `"`}},
	} {
		w := serve(testAdminUI(t, &fakeAdmin{}, &fakeAuthorizer{}), formRequest("GET", scenario.target, nil))
		for _, want := range scenario.want {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatal(scenario.target + " missing: " + want)
			}
		}
	}
}

func TestCreateUserNormalizesUsernameAndHidesPassword(t *testing.T) {
	admin := &fakeAdmin{}
	w := serve(testAdminUI(t, admin, &fakeAuthorizer{}), formRequest("POST", "/admin/users", url.Values{"username": {"  Siti "}, "password": {testPassword}, "platform_admin": {"true"}}))
	if w.Code != http.StatusSeeOther || admin.newUser.Username != "siti" || admin.newUser.Password.Reveal() != testPassword || !admin.newUser.PlatformAdmin {
		t.Fatal("new user not passed correctly")
	}
	admin = &fakeAdmin{err: security.ErrInvalidInput}
	w = serve(testAdminUI(t, admin, &fakeAuthorizer{}), formRequest("POST", "/admin/users", url.Values{"username": {"siti"}, "password": {testPassword}}))
	if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), testPassword) || !strings.Contains(w.Body.String(), "Input tidak valid") {
		t.Fatal("rejected user form leaked password or hid error")
	}
}

func TestCreateConnectionKeepsDSNSecret(t *testing.T) {
	dsn := "postgres://admin:canary-secret@db:5432/app"
	admin := &fakeAdmin{err: security.ErrConflict}
	w := serve(testAdminUI(t, admin, &fakeAuthorizer{}), formRequest("POST", "/admin/connections", url.Values{"engine": {"mysql"}, "name": {"app"}, "dsn": {dsn}}))
	if admin.conn.DSN.Reveal() != dsn || admin.conn.Name != "app" || admin.conn.Engine != dbms.MySQL {
		t.Fatal("connection not passed to service")
	}
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "canary-secret") {
		t.Fatal("DSN echoed back after failure")
	}
}

func TestAdminFormsValidateBeforeCallingService(t *testing.T) {
	for _, route := range []struct {
		target string
		form   url.Values
	}{
		{"/admin/users", url.Values{"username": {"a"}, "password": {testPassword}}},
		{"/admin/users", url.Values{"username": {"siti"}, "password": {""}}},
		{"/admin/users", url.Values{"username": {"siti"}, "password": {testPassword}, "platform_admin": {"yes"}}},
		{"/admin/users/" + string(testUserID) + "/active", url.Values{"active": {""}}},
		{"/admin/users/bukan-uuid/active", url.Values{"active": {"true"}}},
		{"/admin/groups", url.Values{"name": {strings.Repeat("g", 65)}}},
		{memberPath, url.Values{"username": {"bad name"}}},
		{"/admin/connections", url.Values{"name": {"x"}, "dsn": {""}}},
		{"/admin/connections", url.Values{"engine": {"oracle"}, "name": {"x"}, "dsn": {"dsn"}}},
		{"/admin/grants", url.Values{"group_id": {string(testGroupID)}, "database_id": {string(testDatabaseA)}, "access": {"none"}}},
		{"/admin/grants", url.Values{"group_id": {"x"}, "database_id": {string(testDatabaseA)}, "access": {"view_only"}}},
		{"/admin/grants", url.Values{"group_id": {string(testGroupID)}, "database_id": {string(testDatabaseA)}, "access": {"view_only"}, "user_id": {string(testUserID)}}},
	} {
		admin := &fakeAdmin{}
		w := serve(testAdminUI(t, admin, &fakeAuthorizer{}), formRequest("POST", route.target, route.form))
		for _, call := range admin.calls {
			if call != "users" && call != "groups" && call != "connections" && call != "grants" {
				t.Fatal("invalid form reached mutation: " + route.target)
			}
		}
		if w.Code != http.StatusBadRequest {
			t.Fatal("invalid admin form not rejected: " + route.target)
		}
	}
}

func TestAdminMutationsPassParsedValues(t *testing.T) {
	admin := &fakeAdmin{}
	mux := testAdminUI(t, admin, &fakeAuthorizer{})
	serve(mux, formRequest("POST", "/admin/users/"+string(testUserID)+"/active", url.Values{"active": {"false"}}))
	if admin.userID != testUserID || admin.active {
		t.Fatal("user status not passed")
	}
	serve(mux, formRequest("POST", memberPath, url.Values{"username": {" SITI "}}))
	if admin.groupID != testGroupID || admin.username != "siti" {
		t.Fatal("member not passed")
	}
	serve(mux, formRequest("POST", "/admin/grants", url.Values{"group_id": {string(testGroupID)}, "database_id": {string(testDatabaseA)}, "access": {"view_edit"}}))
	if admin.database != testDatabaseA || admin.access != security.AccessViewEdit {
		t.Fatal("grant not passed")
	}
}

func TestAdminUnexpectedErrorIsSanitized(t *testing.T) {
	w := serve(testAdminUI(t, &fakeAdmin{err: errors.New("duplicate key canary-secret")}, &fakeAuthorizer{}), formRequest("GET", "/admin/users", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "canary-secret") {
		t.Fatal("unexpected admin error leaked")
	}
}

func TestCreateUserExplainsPasswordPolicyBeforeService(t *testing.T) {
	for _, scenario := range []struct {
		password string
		message  string
	}{
		{"pendek", "Password minimal 15 karakter"},
		{"", "Password minimal 15 karakter"},
		{strings.Repeat("x", security.MaxPasswordBytes+1), "Password maksimal"},
	} {
		admin := &fakeAdmin{}
		w := serve(testAdminUI(t, admin, &fakeAuthorizer{}), formRequest("POST", "/admin/users", url.Values{"username": {"siti"}, "password": {scenario.password}}))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), scenario.message) {
			t.Fatal("password policy reason not shown: " + scenario.message)
		}
		for _, call := range admin.calls {
			if call == "create-user" {
				t.Fatal("weak password reached service")
			}
		}
		if scenario.password != "" && strings.Contains(w.Body.String(), scenario.password) {
			t.Fatal("rejected password echoed back")
		}
	}
}
