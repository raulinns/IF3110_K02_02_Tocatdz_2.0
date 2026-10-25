package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/pgtest"
)

const (
	origin         = "http://localhost:8080"
	adminPassword  = "password admin demo panjang"
	viewerPassword = "password pengamat demo panjang"
)

var (
	csrfField     = regexp.MustCompile(`name="csrf_token" value="([A-Za-z0-9_-]{43})"`)
	revisionField = regexp.MustCompile(`name="revision" value="(\d+)"`)
	databaseLink  = regexp.MustCompile(`/databases/([0-9a-f-]{36})/schema`)
)

func createDatabase(t *testing.T) string {
	t.Helper()
	return pgtest.Database(t, "TEST_DATABASE_URL")
}

func key(fill byte) security.Secret {
	return security.NewSecret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

type e2e struct {
	handler  http.Handler
	target   *pgx.Conn
	platform *Platform
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	platformURL, targetURL := createDatabase(t), createDatabase(t)
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
	box, _ := postgres.NewSecretBox(sec.ConnectionKey)
	if err := postgres.Seed(ctx, platform.pool, box, security.NewTokenGenerator(), postgres.SeedInput{
		AdminUsername: "admin", AdminHash: adminHash, ViewerUsername: "pengamat", ViewerHash: viewerHash,
		TargetName: "demo", TargetDSN: security.NewSecret(targetURL),
	}); err != nil {
		t.Fatal(err)
	}
	return &e2e{handler: platform.Handler, target: target, platform: platform}
}

func (e *e2e) do(method, target string, form url.Values, cookie string) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, target, body)
	if method == http.MethodPost {
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: cookie})
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

func (e *e2e) login(t *testing.T, username, password string) string {
	t.Helper()
	w := e.do("POST", "/login", url.Values{"username": {username}, "password": {password}}, "")
	if w.Code != http.StatusSeeOther || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login %s failed: %d", username, w.Code)
	}
	return w.Result().Cookies()[0].Value
}

func match(t *testing.T, pattern *regexp.Regexp, body string) string {
	t.Helper()
	found := pattern.FindStringSubmatch(body)
	if found == nil {
		t.Fatalf("pattern %s not found", pattern)
	}
	return found[1]
}

func TestDemoFlowAgainstPostgreSQL(t *testing.T) {
	e := newE2E(t)
	if w := e.do("GET", "/login", nil, ""); w.Code != http.StatusOK {
		t.Fatal("login page unavailable")
	}
	if w := e.do("POST", "/login", url.Values{"username": {"admin"}, "password": {"password yang salah sekali"}}, ""); w.Code != http.StatusUnauthorized {
		t.Fatal("wrong password accepted")
	}
	cookie := e.login(t, "admin", adminPassword)
	w := e.do("GET", "/databases", nil, cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "demo") || !strings.Contains(w.Body.String(), `href="/admin/users"`) {
		t.Fatal("database list or admin link missing")
	}
	database := match(t, databaseLink, w.Body.String())
	base := "/databases/" + database
	if w := e.do("GET", base+"/schema", nil, cookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pelanggan") {
		t.Fatal("schema page missing table")
	}
	if w := e.do("GET", base+"/tables/public/pelanggan", nil, cookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pelanggan_pkey") {
		t.Fatal("table page missing catalog detail")
	}
	if w := e.do("GET", base+"/tables/public/pelanggan/rows", nil, cookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Siti") {
		t.Fatal("rows page missing data")
	}
	w = e.do("GET", base+"/migrations/new", nil, cookie)
	token := match(t, csrfField, w.Body.String())
	w = e.do("POST", base+"/migrations", url.Values{"csrf_token": {token}, "title": {"Tambah telepon"}, "sql": {"ALTER TABLE public.pelanggan ADD COLUMN telepon text;"}}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("draft not created: %d", w.Code)
	}
	detail := w.Header().Get("Location")
	revision := match(t, revisionField, e.do("GET", detail, nil, cookie).Body.String())
	if w := e.do("POST", detail+"/execute", url.Values{"csrf_token": {token}, "revision": {revision}}, cookie); w.Code != http.StatusSeeOther {
		t.Fatalf("execute failed: %d", w.Code)
	}
	if w := e.do("GET", detail, nil, cookie); !strings.Contains(w.Body.String(), "Berhasil") {
		t.Fatal("applied status not shown")
	}
	var column bool
	if err := e.target.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'pelanggan' AND column_name = 'telepon')").Scan(&column); err != nil || !column {
		t.Fatal("migration not committed to target")
	}
	w = e.do("POST", base+"/migrations", url.Values{"csrf_token": {token}, "title": {"Gagal"}, "sql": {"CREATE TABLE public.sementara (id int); INSERT INTO public.tidak_ada VALUES (1);"}}, cookie)
	failed := w.Header().Get("Location")
	revision = match(t, revisionField, e.do("GET", failed, nil, cookie).Body.String())
	e.do("POST", failed+"/execute", url.Values{"csrf_token": {token}, "revision": {revision}}, cookie)
	if w := e.do("GET", failed, nil, cookie); !strings.Contains(w.Body.String(), "Gagal") {
		t.Fatal("failed status not shown")
	}
	var leftover bool
	if err := e.target.QueryRow(context.Background(), "SELECT to_regclass('public.sementara') IS NOT NULL").Scan(&leftover); err != nil || leftover {
		t.Fatal("failed migration was not rolled back")
	}
	if w := e.do("GET", base+"/migrations", nil, cookie); !strings.Contains(w.Body.String(), "Tambah telepon") || !strings.Contains(w.Body.String(), "admin") {
		t.Fatal("history missing executed migration")
	}
	if w := e.do("POST", "/admin/groups", url.Values{"csrf_token": {token}, "name": {"tim-baru"}}, cookie); w.Code != http.StatusSeeOther {
		t.Fatalf("admin could not create group: %d", w.Code)
	}
	if w := e.do("GET", "/admin/groups", nil, cookie); !strings.Contains(w.Body.String(), "tim-baru") || !strings.Contains(w.Body.String(), "pengembang") {
		t.Fatal("admin groups page incomplete")
	}
	if w := e.do("POST", "/logout", url.Values{"csrf_token": {token}}, cookie); w.Code != http.StatusSeeOther {
		t.Fatal("logout failed")
	}
	if w := e.do("GET", "/databases", nil, cookie); w.Code != http.StatusUnauthorized {
		t.Fatal("session alive after logout")
	}
}

func TestViewOnlyUserCannotChangeAnything(t *testing.T) {
	e := newE2E(t)
	cookie := e.login(t, "pengamat", viewerPassword)
	w := e.do("GET", "/databases", nil, cookie)
	if !strings.Contains(w.Body.String(), "Lihat saja") || strings.Contains(w.Body.String(), `href="/admin/users"`) {
		t.Fatal("viewer list wrong")
	}
	token := match(t, csrfField, w.Body.String())
	base := "/databases/" + match(t, databaseLink, w.Body.String())
	if w := e.do("GET", base+"/tables/public/pelanggan/rows", nil, cookie); w.Code != http.StatusOK {
		t.Fatal("viewer cannot read data")
	}
	if w := e.do("GET", base+"/migrations", nil, cookie); w.Code != http.StatusOK || strings.Contains(w.Body.String(), base+"/migrations/new") {
		t.Fatal("viewer offered migration editor")
	}
	if w := e.do("POST", base+"/migrations", url.Values{"csrf_token": {token}, "title": {"x"}, "sql": {"DROP TABLE public.pelanggan;"}}, cookie); w.Code != http.StatusForbidden {
		t.Fatalf("viewer created migration: %d", w.Code)
	}
	if w := e.do("GET", "/admin/users", nil, cookie); w.Code != http.StatusForbidden {
		t.Fatal("viewer opened admin page")
	}
	var exists bool
	if err := e.target.QueryRow(context.Background(), "SELECT to_regclass('public.pelanggan') IS NOT NULL").Scan(&exists); err != nil || !exists {
		t.Fatal("viewer changed target")
	}
}
