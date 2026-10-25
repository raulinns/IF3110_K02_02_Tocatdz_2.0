package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/mysqltest"
)

const ungrantedPassword = "password tanpa akses demo"

func TestMySQLFlowThroughAdminAccessViewerAndMigration(t *testing.T) {
	mysqlDSN := mysqltest.Database(t, "TEST_MYSQL_TARGET_DSN")

	ctx := context.Background()
	platformURL, seedTargetURL := createDatabase(t), createDatabase(t)
	sec := config.Security{PublicOrigin: origin, SessionKey: key(11), CSRFKey: key(12), ConnectionKey: key(13)}
	platform, err := Build(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Config{
		AppEnv: "testing", DatabaseURL: platformURL,
	}, sec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(platform.Close)

	hasher, err := NewPasswordHasher()
	if err != nil {
		t.Fatal(err)
	}
	adminHash, err := hasher.Hash(ctx, security.NewSecret(adminPassword))
	if err != nil {
		t.Fatal(err)
	}
	viewerHash, err := hasher.Hash(ctx, security.NewSecret(viewerPassword))
	if err != nil {
		t.Fatal(err)
	}
	box, err := postgres.NewSecretBox(sec.ConnectionKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.Seed(ctx, platform.pool, box, security.NewTokenGenerator(), postgres.SeedInput{
		AdminUsername: "admin", AdminHash: adminHash, ViewerUsername: "pengamat", ViewerHash: viewerHash,
		TargetName: "postgres-seed", TargetDSN: security.NewSecret(seedTargetURL),
	}); err != nil {
		t.Fatal(err)
	}

	mysqlDB, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlDB.Close() })
	if err := mysqlDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := mysqlDB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil || schema == "" {
		t.Fatalf("read MySQL schema: %q, %v", schema, err)
	}
	for _, statement := range []string{
		"DROP TABLE IF EXISTS `mysql_e2e_people`",
		"CREATE TABLE `mysql_e2e_people` (`id` BIGINT PRIMARY KEY, `name` VARCHAR(100) NOT NULL)",
		"INSERT INTO `mysql_e2e_people` (`id`, `name`) VALUES (1, 'Siti')",
	} {
		if _, err := mysqlDB.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = mysqlDB.Exec("DROP TABLE IF EXISTS `mysql_e2e_people`")
		_, _ = mysqlDB.Exec("DROP TABLE IF EXISTS `mysql_e2e_uncertain`")
		_, _ = mysqlDB.Exec("DROP TABLE IF EXISTS `_tocatdz_migration_receipts`")
	})

	e := &e2e{handler: platform.Handler, platform: platform}
	adminCookie := e.login(t, "admin", adminPassword)
	connections := e.do("GET", "/admin/connections", nil, adminCookie)
	csrf := match(t, csrfField, connections.Body.String())
	if w := e.do("POST", "/admin/connections", url.Values{
		"csrf_token": {csrf}, "engine": {"mysql"}, "name": {"mysql-e2e"}, "dsn": {mysqlDSN},
	}, adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("create MySQL connection: status=%d body=%s", w.Code, w.Body.String())
	}

	var databaseID string
	var engine string
	var ciphertext []byte
	if err := platform.pool.QueryRow(ctx, `SELECT id::text, engine, dsn_ciphertext
		FROM managed_databases WHERE name = 'mysql-e2e'`).Scan(&databaseID, &engine, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if engine != "mysql" || len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte(mysqlDSN)) {
		t.Fatal("MySQL metadata was not stored with the expected engine and encrypted DSN")
	}

	if w := e.do("POST", "/admin/users", url.Values{
		"csrf_token": {csrf}, "username": {"tanpaakses"}, "password": {ungrantedPassword},
	}, adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("create ungranted user: status=%d", w.Code)
	}

	var developerGroup, viewerGroup string
	if err := platform.pool.QueryRow(ctx, "SELECT id::text FROM groups WHERE name = 'pengembang'").Scan(&developerGroup); err != nil {
		t.Fatal(err)
	}
	if err := platform.pool.QueryRow(ctx, "SELECT id::text FROM groups WHERE name = 'pengamat'").Scan(&viewerGroup); err != nil {
		t.Fatal(err)
	}
	if w := e.do("POST", "/admin/grants", url.Values{
		"csrf_token": {csrf}, "group_id": {developerGroup}, "database_id": {databaseID}, "access": {"view_edit"},
	}, adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("grant developer access: status=%d", w.Code)
	}

	adminCookie = e.login(t, "admin", adminPassword)
	csrf = match(t, csrfField, e.do("GET", "/admin/grants", nil, adminCookie).Body.String())
	if w := e.do("POST", "/admin/grants", url.Values{
		"csrf_token": {csrf}, "group_id": {viewerGroup}, "database_id": {databaseID}, "access": {"view_only"},
	}, adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("grant viewer access: status=%d", w.Code)
	}

	base := "/databases/" + databaseID
	ungrantedCookie := e.login(t, "tanpaakses", ungrantedPassword)
	if w := e.do("GET", base+"/schema", nil, ungrantedCookie); w.Code != http.StatusForbidden {
		t.Fatalf("ungranted user opened MySQL database: status=%d", w.Code)
	}
	viewerCookie := e.login(t, "pengamat", viewerPassword)
	schemaPage := e.do("GET", base+"/schema", nil, viewerCookie)
	if schemaPage.Code != http.StatusOK || strings.Contains(schemaPage.Body.String(), base+"/schema-diff") {
		t.Fatalf("MySQL page exposed PostgreSQL-only schema diff: status=%d", schemaPage.Code)
	}
	if w := e.do("GET", base+"/schema-diff", nil, viewerCookie); w.Code != http.StatusNotFound {
		t.Fatalf("MySQL schema diff route is reachable: status=%d", w.Code)
	}
	if w := e.do("GET", base+"/tables/"+url.PathEscape(schema)+"/mysql_e2e_people/rows", nil, viewerCookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Siti") {
		t.Fatalf("viewer could not read MySQL rows: status=%d", w.Code)
	}

	adminCookie = e.login(t, "admin", adminPassword)
	newMigration := e.do("GET", base+"/migrations/new", nil, adminCookie)
	csrf = match(t, csrfField, newMigration.Body.String())
	created := e.do("POST", base+"/migrations", url.Values{
		"csrf_token": {csrf}, "title": {"Tambah email MySQL"},
		"sql": {"ALTER TABLE `mysql_e2e_people` ADD COLUMN `email` VARCHAR(255);"},
	}, adminCookie)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create MySQL migration: status=%d body=%s", created.Code, created.Body.String())
	}
	detail := created.Header().Get("Location")
	detailPage := e.do("GET", detail, nil, adminCookie)
	if strings.Contains(detailPage.Body.String(), "Analisis query") {
		t.Fatal("MySQL migration exposed PostgreSQL-only execution plan")
	}
	revision := match(t, revisionField, detailPage.Body.String())
	if w := e.do("POST", detail+"/plan", url.Values{
		"csrf_token": {csrf}, "revision": {revision}, "statement": {"1"}, "mode": {"explain"},
	}, adminCookie); w.Code != http.StatusNotFound {
		t.Fatalf("MySQL execution plan route is reachable: status=%d", w.Code)
	}
	if w := e.do("POST", detail+"/execute", url.Values{
		"csrf_token": {csrf}, "revision": {revision},
	}, adminCookie); w.Code != http.StatusSeeOther {
		t.Fatalf("execute MySQL migration: status=%d body=%s", w.Code, w.Body.String())
	}
	if w := e.do("GET", detail, nil, adminCookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Berhasil") {
		t.Fatal("applied MySQL migration status is missing")
	}
	var columnCount int
	if err := mysqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'mysql_e2e_people' AND COLUMN_NAME = 'email'`).Scan(&columnCount); err != nil || columnCount != 1 {
		t.Fatalf("MySQL migration result missing: count=%d err=%v", columnCount, err)
	}
	if w := e.do("GET", base+"/migrations", nil, adminCookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Tambah email MySQL") {
		t.Fatal("MySQL migration history is missing the applied migration")
	}

	uncertainDraft := e.do("POST", base+"/migrations", url.Values{
		"csrf_token": {csrf}, "title": {"DDL MySQL terputus"},
		"sql": {"CREATE TABLE `mysql_e2e_uncertain` (`id` BIGINT PRIMARY KEY); INSERT INTO `mysql_e2e_missing` VALUES (1);"},
	}, adminCookie)
	if uncertainDraft.Code != http.StatusSeeOther {
		t.Fatalf("create uncertain MySQL migration: status=%d", uncertainDraft.Code)
	}
	uncertainDetail := uncertainDraft.Header().Get("Location")
	uncertainRevision := match(t, revisionField, e.do("GET", uncertainDetail, nil, adminCookie).Body.String())
	if w := e.do("POST", uncertainDetail+"/execute", url.Values{
		"csrf_token": {csrf}, "revision": {uncertainRevision},
	}, adminCookie); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("implicit-commit uncertainty was not surfaced: status=%d body=%s", w.Code, w.Body.String())
	}
	if w := e.do("GET", uncertainDetail, nil, adminCookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "menunggu konfirmasi") {
		t.Fatal("uncertain MySQL migration was not kept pending")
	}
	var uncertainTable, startedReceipt int
	if err := mysqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'mysql_e2e_uncertain'`).Scan(&uncertainTable); err != nil || uncertainTable != 1 {
		t.Fatalf("test did not reproduce MySQL implicit DDL commit: count=%d err=%v", uncertainTable, err)
	}
	if err := mysqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM _tocatdz_migration_receipts
		WHERE status = 'started'`).Scan(&startedReceipt); err != nil || startedReceipt != 1 {
		t.Fatalf("uncertain execution intent is missing: count=%d err=%v", startedReceipt, err)
	}
}
