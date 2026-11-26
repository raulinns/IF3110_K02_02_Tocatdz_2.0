package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/mysqltest"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/pgtest"
)

const (
	origin         = "http://localhost:8080"
	adminPassword  = "password admin demo panjang"
	viewerPassword = "password pengamat demo panjang"
	editorPassword = "password editor demo panjang"
)

func key(fill byte) security.Secret {
	return security.NewSecret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

type e2e struct {
	handler  http.Handler
	target   *pgx.Conn
	platform *Platform
}

type session struct {
	cookie, csrf string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	platformURL, targetURL := pgtest.Database(t, "TEST_DATABASE_URL"), pgtest.Database(t, "TEST_DATABASE_URL")
	ctx := context.Background()
	sec := config.Security{PublicOrigin: origin, SessionKey: key(1), CSRFKey: key(2), ConnectionKey: key(3)}
	platform, err := Build(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Config{AppEnv: "testing", DatabaseURL: platformURL}, sec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(platform.Close)
	target, err := pgx.Connect(ctx, targetURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.Close(context.Background()) })
	if _, err := target.Exec(ctx, "CREATE TABLE public.pelanggan (id serial PRIMARY KEY, nama text NOT NULL); INSERT INTO public.pelanggan (nama) VALUES ('Budi'), ('Siti');"); err != nil {
		t.Fatal(err)
	}
	hasher, _ := NewPasswordHasher()
	adminHash, _ := hasher.Hash(ctx, security.NewSecret(adminPassword))
	viewerHash, _ := hasher.Hash(ctx, security.NewSecret(viewerPassword))
	editorHash, _ := hasher.Hash(ctx, security.NewSecret(editorPassword))
	box, _ := postgres.NewSecretBox(sec.ConnectionKey)
	if err := postgres.Seed(ctx, platform.pool, box, security.NewTokenGenerator(), postgres.SeedInput{
		AdminUsername: "admin", AdminHash: adminHash, ViewerUsername: "pengamat", ViewerHash: viewerHash,
		EditorUsername: "editor", EditorHash: editorHash,
		TargetName: "demo", TargetDSN: security.NewSecret(targetURL),
	}); err != nil {
		t.Fatal(err)
	}
	return &e2e{handler: platform.Handler, target: target, platform: platform}
}

func (e *e2e) call(t *testing.T, s *session, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, path, reader)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if !security.SafeMethod(method) {
		r.Header.Set("Origin", origin)
	}
	if s != nil {
		r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: s.cookie})
		if s.csrf != "" && !security.SafeMethod(method) {
			r.Header.Set("X-CSRF-Token", s.csrf)
		}
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	if s != nil {
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == security.SessionCookieName && cookie.Value != "" {
				s.cookie = cookie.Value
			}
		}
	}
	var decoded map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &decoded)
	return w.Code, decoded
}

func (e *e2e) login(t *testing.T, username, password string) *session {
	t.Helper()
	s := &session{}
	status, _ := e.call(t, s, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": username, "password": password})
	if status != http.StatusOK || s.cookie == "" {
		t.Fatalf("login %s failed: %d", username, status)
	}
	status, me := e.call(t, s, http.MethodGet, "/api/v1/auth/me", nil)
	token, _ := me["csrf_token"].(string)
	if status != http.StatusOK || token == "" {
		t.Fatalf("auth/me for %s failed: %d", username, status)
	}
	s.csrf = token
	return s
}

func items(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("response without items: %v", body)
	}
	result := make([]map[string]any, len(raw))
	for i, item := range raw {
		result[i], _ = item.(map[string]any)
	}
	return result
}

func find(t *testing.T, list []map[string]any, field, value string) map[string]any {
	t.Helper()
	for _, item := range list {
		if item[field] == value {
			return item
		}
	}
	t.Fatalf("%s=%s not found", field, value)
	return nil
}

func TestServerServesSPAAndHealth(t *testing.T) {
	e := newE2E(t)
	for _, path := range []string{"/", "/login", "/databases/abc/schema", "/healthz"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		e.handler.ServeHTTP(w, r)
		// Tanpa hasil build web/dist, handler SPA menampilkan halaman placeholder dengan status 503.
		if w.Code != http.StatusOK && !(w.Code == http.StatusServiceUnavailable && strings.HasPrefix(w.Header().Get("Content-Type"), "text/html")) {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
	}
	if status, _ := e.call(t, nil, http.MethodGet, "/api/v1/databases", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous API access = %d", status)
	}
}

func TestAPIDemoFlowAgainstPostgreSQL(t *testing.T) {
	e := newE2E(t)
	if status, _ := e.call(t, &session{}, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "password yang salah sekali"}); status != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d", status)
	}
	admin := e.login(t, "admin", adminPassword)

	status, body := e.call(t, admin, http.MethodGet, "/api/v1/databases", nil)
	if status != http.StatusOK {
		t.Fatalf("databases = %d", status)
	}
	demo := find(t, items(t, body), "name", "demo")
	database := demo["id"].(string)
	status, body = e.call(t, admin, http.MethodGet, "/api/v1/databases/"+database+"/schema", nil)
	if status != http.StatusOK || !strings.Contains(toJSON(body), "pelanggan") {
		t.Fatalf("schema = %d %v", status, body)
	}
	status, body = e.call(t, admin, http.MethodGet, "/api/v1/databases/"+database+"/tables/public/pelanggan/rows", nil)
	if status != http.StatusOK || len(items(t, body)) != 2 {
		t.Fatalf("rows = %d %v", status, body)
	}

	withoutCSRF := &session{cookie: admin.cookie}
	if status, _ := e.call(t, withoutCSRF, http.MethodPost, "/api/v1/admin/groups", map[string]string{"name": "tanpa-csrf"}); status != http.StatusForbidden {
		t.Fatalf("mutation without CSRF = %d", status)
	}
	status, group := e.call(t, admin, http.MethodPost, "/api/v1/admin/groups", map[string]string{"name": "tim-baru"})
	if status != http.StatusCreated {
		t.Fatalf("create group = %d %v", status, group)
	}
	groupID := group["id"].(string)
	_, users := e.call(t, admin, http.MethodGet, "/api/v1/admin/users", nil)
	viewer := find(t, items(t, users), "username", "pengamat")["id"].(string)

	if status, body := e.call(t, admin, http.MethodPut, "/api/v1/admin/groups/"+groupID+"/members/"+viewer, nil); status >= 300 {
		t.Fatalf("add member = %d %v", status, body)
	}
	if status, body := e.call(t, admin, http.MethodPut, "/api/v1/admin/groups/"+groupID+"/leader", map[string]string{"user_id": viewer}); status != http.StatusOK {
		t.Fatalf("set leader = %d %v", status, body)
	}
	if status, body := e.call(t, admin, http.MethodPut, "/api/v1/admin/grants/"+groupID+"/"+database, map[string]string{"access": "view_only"}); status != http.StatusOK {
		t.Fatalf("put grant = %d %v", status, body)
	}
	_, groups := e.call(t, admin, http.MethodGet, "/api/v1/admin/groups", nil)
	if !strings.Contains(toJSON(find(t, items(t, groups), "id", groupID)), viewer) {
		t.Fatalf("group leader missing: %v", groups)
	}
	if status, _ := e.call(t, admin, http.MethodDelete, "/api/v1/admin/groups/"+groupID+"/members/"+viewer, nil); status != http.StatusConflict {
		t.Fatalf("remove current leader = %d", status)
	}

	pengamat := e.login(t, "pengamat", viewerPassword)
	status, me := e.call(t, pengamat, http.MethodGet, "/api/v1/auth/me", nil)
	if status != http.StatusOK || !strings.Contains(toJSON(me["leader_of"]), groupID) {
		t.Fatalf("leader_of = %v", me["leader_of"])
	}
	if status, _ := e.call(t, admin, http.MethodPost, "/api/v1/auth/logout", nil); status >= 300 {
		t.Fatalf("logout = %d", status)
	}
	if status, _ := e.call(t, admin, http.MethodGet, "/api/v1/databases", nil); status != http.StatusUnauthorized {
		t.Fatalf("revoked session still works: %d", status)
	}
}

func TestAPIViewOnlyUserCannotChangeAnything(t *testing.T) {
	e := newE2E(t)
	viewer := e.login(t, "pengamat", viewerPassword)
	status, body := e.call(t, viewer, http.MethodGet, "/api/v1/databases", nil)
	if status != http.StatusOK {
		t.Fatalf("databases = %d", status)
	}
	demo := find(t, items(t, body), "name", "demo")
	if demo["access"] != "view_only" {
		t.Fatalf("access = %v", demo["access"])
	}
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/admin/users", nil},
		{http.MethodPost, "/api/v1/admin/groups", map[string]string{"name": "x"}},
		{http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "baru", "password": "password baru cukup panjang"}},
	} {
		if status, _ := e.call(t, viewer, request.method, request.path, request.body); status != http.StatusForbidden {
			t.Fatalf("%s %s = %d", request.method, request.path, status)
		}
	}
}

func TestAPISchemaDiffBetweenSchemas(t *testing.T) {
	if raceEnabled {
		t.Skip("race detector melambatkan perbandingan melewati schemadiff.CompareTimeout")
	}
	e := newE2E(t)
	if _, err := e.target.Exec(context.Background(), `CREATE SCHEMA rancangan;
		CREATE TABLE rancangan.pelanggan (id serial PRIMARY KEY, nama text NOT NULL, aktif boolean NOT NULL DEFAULT true);`); err != nil {
		t.Fatal(err)
	}
	admin := e.login(t, "admin", adminPassword)
	_, body := e.call(t, admin, http.MethodGet, "/api/v1/databases", nil)
	database := find(t, items(t, body), "name", "demo")["id"].(string)
	status, diff := e.call(t, admin, http.MethodGet, "/api/v1/databases/"+database+"/schema-diff?desired_database_id="+database+"&current_schema=public&desired_schema=rancangan", nil)
	if status != http.StatusOK || !strings.Contains(toJSON(diff), "aktif") {
		t.Fatalf("schema diff = %d %v", status, diff)
	}
}

func TestAPIMySQLConnectionAndExplorer(t *testing.T) {
	mysqlDSN := mysqltest.Database(t, "TEST_MYSQL_TARGET_DSN")
	e := newE2E(t)
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS `api_e2e_people`"); _ = db.Close() })
	for _, statement := range []string{
		"DROP TABLE IF EXISTS `api_e2e_people`",
		"CREATE TABLE `api_e2e_people` (`id` BIGINT PRIMARY KEY, `name` VARCHAR(100) NOT NULL)",
		"INSERT INTO `api_e2e_people` (`id`, `name`) VALUES (1, 'Siti')",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	admin := e.login(t, "admin", adminPassword)
	status, connection := e.call(t, admin, http.MethodPost, "/api/v1/admin/connections", map[string]string{"name": "mysql-e2e", "engine": "mysql", "dsn": mysqlDSN})
	if status != http.StatusCreated {
		t.Fatalf("create MySQL connection = %d %v", status, connection)
	}
	databaseID := connection["id"].(string)
	_, group := e.call(t, admin, http.MethodPost, "/api/v1/admin/groups", map[string]string{"name": "tim-mysql"})
	groupID := group["id"].(string)
	_, users := e.call(t, admin, http.MethodGet, "/api/v1/admin/users", nil)
	adminID := find(t, items(t, users), "username", "admin")["id"].(string)
	if status, body := e.call(t, admin, http.MethodPut, "/api/v1/admin/groups/"+groupID+"/members/"+adminID, nil); status >= 300 {
		t.Fatalf("add admin to group = %d %v", status, body)
	}
	admin = e.login(t, "admin", adminPassword)
	if status, body := e.call(t, admin, http.MethodPut, "/api/v1/admin/grants/"+groupID+"/"+databaseID, map[string]string{"access": "view_edit"}); status != http.StatusOK {
		t.Fatalf("grant MySQL = %d %v", status, body)
	}
	admin = e.login(t, "admin", adminPassword)
	_, body := e.call(t, admin, http.MethodGet, "/api/v1/databases", nil)
	if find(t, items(t, body), "id", databaseID)["engine"] != "mysql" {
		t.Fatalf("MySQL database missing: %v", body)
	}
	status, schema := e.call(t, admin, http.MethodGet, "/api/v1/databases/"+databaseID+"/schema", nil)
	if status != http.StatusOK || !strings.Contains(toJSON(schema), "api_e2e_people") {
		t.Fatalf("MySQL schema = %d %v", status, schema)
	}
}

func toJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (e *e2e) leaderGroup(t *testing.T, s *session) string {
	t.Helper()
	status, me := e.call(t, s, http.MethodGet, "/api/v1/auth/me", nil)
	groups, _ := me["leader_of"].([]any)
	if status != http.StatusOK || len(groups) != 1 {
		t.Fatalf("leader_of = %v (status %d)", me["leader_of"], status)
	}
	return groups[0].(string)
}

func (e *e2e) tableExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	if err := e.target.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", "public."+name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

// submit mengajukan perubahan, disetujui kedua leader, lalu dieksekusi worker server; hasilnya job yang sudah selesai.
func (e *e2e) submitAndRun(t *testing.T, editor, admin, viewer *session, database string, change map[string]any) map[string]any {
	t.Helper()
	path := "/api/v1/databases/" + database + "/change-requests"
	status, request := e.call(t, editor, http.MethodPost, path, change)
	if status != http.StatusCreated || request["status"] != "pending_review" {
		t.Fatalf("submit = %d %v", status, request)
	}
	for _, leader := range []*session{admin, viewer} {
		status, request = e.call(t, leader, http.MethodPost, path+"/"+request["id"].(string)+"/approvals", map[string]any{
			"revision": request["revision"], "group_id": e.leaderGroup(t, leader), "decision": "approve", "note": "ok"})
		if status != http.StatusOK {
			t.Fatalf("approval = %d %v", status, request)
		}
	}
	if request["status"] != "approved" {
		t.Fatalf("status after both approvals = %v", request["status"])
	}
	status, accepted := e.call(t, editor, http.MethodPost, path+"/"+request["id"].(string)+"/execute", map[string]any{"revision": request["revision"]})
	if status != http.StatusAccepted || accepted["job_id"] == nil {
		t.Fatalf("execute = %d %v", status, accepted)
	}
	// Eksekusi tidak terjadi di request HTTP: worker mengambil job dari queue.
	if _, err := e.platform.m2Worker.ProcessOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, job := e.call(t, editor, http.MethodGet, "/api/v1/jobs/"+accepted["job_id"].(string), nil)
	if status != http.StatusOK || job["status"] != "succeeded" {
		t.Fatalf("job = %d %v", status, job)
	}
	return job
}

func TestAPIWorkflowAppliesRollsBackAndHidesRollback(t *testing.T) {
	e := newE2E(t)
	editor, admin, viewer := e.login(t, "editor", editorPassword), e.login(t, "admin", adminPassword), e.login(t, "pengamat", viewerPassword)
	_, body := e.call(t, editor, http.MethodGet, "/api/v1/databases", nil)
	database := find(t, items(t, body), "name", "demo")["id"].(string)
	base := "/api/v1/databases/" + database

	status, migrationBody := e.call(t, editor, http.MethodPost, base+"/migrations", map[string]any{
		"title": "tabel baru", "up_sql": "CREATE TABLE public.alur_api (id int)", "down_sql": "DROP TABLE public.alur_api",
		"irreversible": false, "irreversible_reason": "", "mode": "normal", "batch": nil})
	if status != http.StatusCreated {
		t.Fatalf("create migration = %d %v", status, migrationBody)
	}
	e.submitAndRun(t, editor, admin, viewer, database, map[string]any{
		"migration_id": migrationBody["id"], "migration_revision": migrationBody["revision"], "kind": "apply"})
	if !e.tableExists(t, "alur_api") {
		t.Fatal("apply did not change the target database")
	}

	status, history := e.call(t, editor, http.MethodGet, base+"/executions", nil)
	executions := items(t, history)
	if status != http.StatusOK || len(executions) != 1 {
		t.Fatalf("history after apply = %d %v", status, history)
	}
	status, position := e.call(t, editor, http.MethodGet, base+"/position", nil)
	if status != http.StatusOK {
		t.Fatalf("position = %d %v", status, position)
	}

	e.submitAndRun(t, editor, admin, viewer, database, map[string]any{
		"migration_id": migrationBody["id"], "migration_revision": migrationBody["revision"], "kind": "rollback",
		"target_execution_id": executions[0]["id"]})
	if e.tableExists(t, "alur_api") {
		t.Fatal("rollback did not undo the change")
	}
	_, full := e.call(t, editor, http.MethodGet, base+"/executions", nil)
	_, hidden := e.call(t, editor, http.MethodGet, base+"/executions?hide_rollback=true", nil)
	if len(items(t, full)) != 2 || len(items(t, hidden)) != 0 {
		t.Fatalf("history full=%d hidden=%d, want 2 and 0", len(items(t, full)), len(items(t, hidden)))
	}

	// Pengguna tanpa hak ubah tidak bisa membuat migration, dan editor tidak punya hak admin.
	if status, _ := e.call(t, viewer, http.MethodPost, base+"/migrations", map[string]any{
		"title": "x", "up_sql": "SELECT 1", "down_sql": "SELECT 1", "irreversible": false, "irreversible_reason": "", "mode": "normal", "batch": nil}); status != http.StatusForbidden {
		t.Fatalf("view-only create migration = %d", status)
	}
	if status, _ := e.call(t, editor, http.MethodGet, "/api/v1/admin/users", nil); status != http.StatusForbidden {
		t.Fatalf("editor reading admin users = %d", status)
	}
}
