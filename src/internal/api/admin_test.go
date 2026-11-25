package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	adminTestGroup  security.ID = "40000000-0000-4000-8000-000000000001"
	adminTestMember security.ID = "30000000-0000-4000-8000-000000000002"
)

type adminPersistenceFixture struct {
	admin.Repository
	admin.M2Repository
	calls    int
	password security.Secret
	active   *bool
	leader   security.ID
}

func (p *adminPersistenceFixture) Users(context.Context) ([]admin.User, error) {
	p.calls++
	return []admin.User{{ID: testUser, Username: "zeta", Active: true}, {ID: adminTestMember, Username: "alpha", Active: true}}, nil
}

func (p *adminPersistenceFixture) CreateUser(_ context.Context, _ security.Principal, _ admin.User, hash security.Secret) error {
	p.calls++
	p.password = hash
	return nil
}

func (p *adminPersistenceFixture) CreateGroup(context.Context, security.Principal, admin.Group) error {
	p.calls++
	return nil
}

func (p *adminPersistenceFixture) CreateConnection(context.Context, security.Principal, admin.Connection, security.Secret) error {
	p.calls++
	return nil
}

func (p *adminPersistenceFixture) Connections(context.Context) ([]admin.Connection, error) {
	p.calls++
	return []admin.Connection{{ID: testDatabase, Name: "Main", Engine: dbms.PostgreSQL}}, nil
}

func (p *adminPersistenceFixture) Grants(context.Context) ([]admin.Grant, error) {
	p.calls++
	return []admin.Grant{{GroupID: adminTestGroup, GroupName: "Engineering", DatabaseID: testDatabase, DatabaseName: "Main", Access: security.AccessViewOnly}}, nil
}

func (p *adminPersistenceFixture) RevokeGrant(context.Context, security.Principal, security.ID, security.ID) error {
	p.calls++
	return nil
}

func (p *adminPersistenceFixture) GroupSummaries(context.Context) ([]admin.GroupSummary, error) {
	p.calls++
	return []admin.GroupSummary{{ID: adminTestGroup, Name: "Engineering", LeaderID: p.leader, MemberCount: 1}}, nil
}

func (p *adminPersistenceFixture) GroupMembers(context.Context, security.ID) ([]admin.User, error) {
	p.calls++
	return []admin.User{{ID: adminTestMember, Username: "member", Active: true}}, nil
}

func (p *adminPersistenceFixture) RenameGroup(_ context.Context, _ security.Principal, group security.ID, name string) (admin.GroupSummary, error) {
	p.calls++
	return admin.GroupSummary{ID: group, Name: name, LeaderID: p.leader, MemberCount: 1}, nil
}

func (p *adminPersistenceFixture) DeleteGroup(context.Context, security.Principal, security.ID) error {
	p.calls++
	return nil
}

func (p *adminPersistenceFixture) DeleteConnection(context.Context, security.Principal, security.ID) error {
	p.calls++
	return security.ErrConflict
}

func (p *adminPersistenceFixture) UpdateUser(_ context.Context, _ security.Principal, user security.ID, active *bool, hash security.Secret) (admin.User, error) {
	p.calls++
	p.active, p.password = active, hash
	return admin.User{ID: user, Username: "member", Active: active == nil || *active}, nil
}

func (p *adminPersistenceFixture) SetLeader(_ context.Context, _ security.Principal, group, user security.ID) (admin.GroupSummary, bool, error) {
	p.calls++
	if user != adminTestMember {
		return admin.GroupSummary{}, false, security.ErrInvalidInput
	}
	changed := p.leader != user
	p.leader = user
	return admin.GroupSummary{ID: group, Name: "Engineering", LeaderID: user, MemberCount: 1}, changed, nil
}

func (p *adminPersistenceFixture) RemoveMember(_ context.Context, _ security.Principal, _ security.ID, user security.ID) error {
	p.calls++
	if p.leader == user {
		return security.ErrConflict
	}
	return nil
}

func (p *adminPersistenceFixture) AddMemberByID(context.Context, security.Principal, security.ID, security.ID) (bool, error) {
	p.calls++
	return true, nil
}

func (p *adminPersistenceFixture) PutGrant(_ context.Context, _ security.Principal, group, database security.ID, access security.DatabaseAccess) (admin.Grant, bool, error) {
	p.calls++
	return admin.Grant{GroupID: group, GroupName: "Engineering", DatabaseID: database, DatabaseName: "Main", Access: access}, true, nil
}

type adminConnectionChecker struct{}

func (adminConnectionChecker) Check(context.Context, dbms.Engine, security.Secret) error { return nil }

func adminTestAPI(t *testing.T, f *integrationFixture, repo *adminPersistenceFixture, withM2 bool) *API {
	t.Helper()
	hasher, err := security.NewArgon2idHasher(func(password, salt []byte, _, _ uint32, _ uint8, _ uint32) []byte {
		hash := sha256.Sum256(append(append([]byte{}, salt...), password...))
		return hash[:]
	})
	if err != nil {
		t.Fatal(err)
	}
	base, err := admin.NewService(admin.Config{
		Repository: repo, Authorizer: f.authorizer, Passwords: hasher,
		IDs: security.NewTokenGenerator(), Checker: adminConnectionChecker{},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := AdminConfig{Service: base, Logger: f.cfg.Logger}
	if withM2 {
		cfg.M2, err = admin.NewM2Service(base, repo)
		if err != nil {
			t.Fatal(err)
		}
	}
	routes, err := AdminRoutes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if withM2 && len(routes) != 17 {
		t.Fatalf("wanted all 17 K1 operations, got %d", len(routes))
	}
	return buildAPI(t, f, routes...)
}

func TestAdminRoutesRequirePlatformAdminAndCSRF(t *testing.T) {
	f := newFixture(t)
	repo := &adminPersistenceFixture{}
	handler := adminTestAPI(t, f, repo, true)
	session := fixtureLogin(t, f)
	leaderPath := Prefix + adminGroups + "/" + string(adminTestGroup) + "/leader"
	leaderBody := `{"user_id":"` + string(adminTestMember) + `"}`
	for _, tc := range []struct {
		method, path, body string
		cookie, csrf       security.Secret
		want               int
	}{
		{http.MethodGet, Prefix + adminUsers, "", security.Secret{}, security.Secret{}, http.StatusUnauthorized},
		{http.MethodGet, Prefix + adminUsers, "", session.CookieToken, security.Secret{}, http.StatusForbidden},
		{http.MethodPut, leaderPath, leaderBody, session.CookieToken, session.CSRFToken, http.StatusForbidden},
	} {
		got := dispatch(handler, request(tc.method, tc.path, tc.body, tc.cookie, tc.csrf))
		if got.Code != tc.want {
			t.Errorf("%s %s: got status %d, want %d", tc.method, tc.path, got.Code, tc.want)
		}
	}
	f = newFixture(t)
	f.store.account.Account.PlatformAdmin = true
	handler = adminTestAPI(t, f, repo, true)
	session = fixtureLogin(t, f)
	missingCSRF := dispatch(handler, request(http.MethodPut, leaderPath, leaderBody, session.CookieToken, security.Secret{}))
	if missingCSRF.Code != http.StatusForbidden || !strings.Contains(missingCSRF.Body.String(), `"csrf_failed"`) {
		t.Errorf("missing CSRF: status %d, body %s", missingCSRF.Code, missingCSRF.Body)
	}
	if repo.calls != 0 {
		t.Errorf("denied request reached persistence: %d calls", repo.calls)
	}
}

func TestAdminRoutesReturnK1RecordsAndAuditCommittedLeaderChange(t *testing.T) {
	f := newFixture(t)
	f.store.account.Account.PlatformAdmin = true
	repo := &adminPersistenceFixture{}
	handler := adminTestAPI(t, f, repo, true)
	session := fixtureLogin(t, f)
	send := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := dispatch(handler, request(method, Prefix+path, body, session.CookieToken, session.CSRFToken))
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: status=%d cache-control=%q", method, path, w.Code, w.Header().Get("Cache-Control"))
		}
		return w
	}
	page := send(http.MethodGet, adminUsers+"?limit=1", "", http.StatusOK)
	if !strings.Contains(page.Body.String(), `"username":"alpha"`) || !strings.Contains(page.Body.String(), `"has_next":true`) {
		t.Fatalf("unexpected first users page: %s", page.Body)
	}
	empty := send(http.MethodGet, adminUsers+"?limit=1&page=3", "", http.StatusOK)
	if !strings.Contains(empty.Body.String(), `"items":[]`) {
		t.Fatal("empty page must encode items as []")
	}
	send(http.MethodGet, adminUsers+"?limit=1&limit=2", "", http.StatusBadRequest)

	const password = "mock test password value"
	const dsn = "mock connection credential"
	created := send(http.MethodPost, adminUsers, `{"username":" New_User ","password":"`+password+`"}`, http.StatusCreated)
	var user UserResponse
	if err := json.Unmarshal(created.Body.Bytes(), &user); err != nil || !user.Active || user.Username != "new_user" || !security.ValidID(user.ID) {
		t.Fatal("create user did not return the committed public record")
	}
	if created.Header().Get("Location") != Prefix+adminUsers+"/"+string(user.ID) || repo.password.Empty() || repo.password.Reveal() == password {
		t.Fatal("missing resource location or password hash")
	}
	group := send(http.MethodPost, adminGroups, `{"name":" New group "}`, http.StatusCreated)
	if !strings.Contains(group.Body.String(), `"leader_user_id":null`) || !strings.Contains(group.Body.String(), `"member_count":0`) {
		t.Fatal("new groups must start with no leader or members")
	}
	connection := send(http.MethodPost, adminConnections, `{"name":"Target","engine":"postgresql","dsn":"`+dsn+`"}`, http.StatusCreated)
	if !strings.Contains(connection.Body.String(), `"engine":"postgresql"`) {
		t.Fatal("missing public connection engine")
	}
	userPath := adminUsers + "/" + string(adminTestMember)
	send(http.MethodPatch, userPath, `{}`, http.StatusUnprocessableEntity)
	send(http.MethodPatch, userPath, `{"active":null,"password":"`+password+`"}`, http.StatusBadRequest)
	send(http.MethodPatch, adminUsers+"/"+string(testUser), `{"active":false}`, http.StatusConflict)
	updated := send(http.MethodPatch, userPath, `{"active":false,"password":"`+password+`"}`, http.StatusOK)
	if repo.active == nil || *repo.active || repo.password.Empty() || repo.password.Reveal() == password || !strings.Contains(updated.Body.String(), `"active":false`) {
		t.Fatal("expected deactivation and a hashed password in one update")
	}
	groupPath := adminGroups + "/" + string(adminTestGroup)
	leaderPath := groupPath + "/leader"
	leaderBody := `{"user_id":"` + string(adminTestMember) + `"}`
	send(http.MethodPut, leaderPath, `{"user_id":"`+string(testUser)+`"}`, http.StatusUnprocessableEntity)
	send(http.MethodPut, groupPath+"/members/"+string(adminTestMember), `{}`, http.StatusBadRequest)
	send(http.MethodPut, groupPath+"/members/"+string(adminTestMember), "", http.StatusNoContent)
	send(http.MethodPut, leaderPath, leaderBody, http.StatusOK)
	send(http.MethodPut, leaderPath, leaderBody, http.StatusOK)
	send(http.MethodDelete, groupPath+"/members/"+string(adminTestMember), "", http.StatusConflict)
	send(http.MethodGet, groupPath+"/members", "", http.StatusOK)
	listGroups := send(http.MethodGet, adminGroups, "", http.StatusOK)
	if !strings.Contains(listGroups.Body.String(), `"leader_user_id":"`+string(adminTestMember)+`"`) {
		t.Fatal("group list did not reflect current leader")
	}
	grant := send(http.MethodPut, adminGrants+"/"+string(adminTestGroup)+"/"+string(testDatabase), `{"access":"view_only"}`, http.StatusOK)
	if !strings.Contains(grant.Body.String(), `"database_name":"Main"`) {
		t.Fatal("grant response is missing database metadata")
	}
	send(http.MethodGet, adminGrants, "", http.StatusOK)
	send(http.MethodDelete, adminGrants+"/"+string(adminTestGroup)+"/"+string(testDatabase), "", http.StatusNoContent)
	send(http.MethodGet, adminConnections, "", http.StatusOK)
	send(http.MethodDelete, adminConnections+"/"+string(testDatabase), "", http.StatusConflict)
	renamed := send(http.MethodPatch, groupPath, `{"name":" Renamed "}`, http.StatusOK)
	if !strings.Contains(renamed.Body.String(), `"name":"Renamed"`) {
		t.Fatal("group update did not return the committed name")
	}
	send(http.MethodDelete, groupPath, "", http.StatusNoContent)
	logs := f.logs.String()
	if strings.Count(logs, `"event":"admin.leader_changed"`) != 1 || !strings.Contains(logs, `"http_route":"/api/v1/admin/groups/{group_id}/leader"`) {
		t.Fatal("expected one correlated leader event for the committed change")
	}
	for _, output := range []string{created.Body.String(), connection.Body.String(), updated.Body.String(), logs} {
		if strings.Contains(output, password) || strings.Contains(output, dsn) || strings.Contains(output, repo.password.Reveal()) {
			t.Fatal("admin response or audit log exposed a write-only value")
		}
	}
}

func TestAdminRoutesCanRegisterM1Subset(t *testing.T) {
	f := newFixture(t)
	f.store.account.Account.PlatformAdmin = true
	repo := &adminPersistenceFixture{}
	handler := adminTestAPI(t, f, repo, false)
	session := fixtureLogin(t, f)
	users := dispatch(handler, request(http.MethodGet, Prefix+adminUsers, "", session.CookieToken, security.Secret{}))
	leader := dispatch(handler, request(http.MethodPut, Prefix+adminGroups+"/"+string(adminTestGroup)+"/leader", "", session.CookieToken, session.CSRFToken))
	if users.Code != http.StatusOK || leader.Code != http.StatusNotFound || repo.calls != 1 {
		t.Fatalf("unexpected subset result: users=%d leader=%d calls=%d", users.Code, leader.Code, repo.calls)
	}
}
