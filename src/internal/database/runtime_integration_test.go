package database

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformstore"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/pgtest"
)

func TestRuntimeConnectsToPostgreSQLAndMySQLTargets(t *testing.T) {
	platformDSN := os.Getenv("TEST_DATABASE_URL")
	postgresDSN := os.Getenv("TEST_POSTGRES_TARGET_DSN")
	mysqlDSN := os.Getenv("TEST_MYSQL_TARGET_DSN")
	if platformDSN == "" || postgresDSN == "" {
		t.Skip("database integration environment is not configured")
	}
	platformDSN = pgtest.Database(t, "TEST_DATABASE_URL")

	ctx := context.Background()
	pool, err := platformdb.OpenMigrated(ctx, platformdb.Config{URL: platformDSN})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "TRUNCATE migration_history, sessions, group_database_access, user_groups, managed_databases, groups, users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(pool, bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	targets := []struct {
		id     security.ID
		name   string
		engine dbms.Engine
		dsn    string
	}{
		{
			id:     "88888888-8888-4888-8888-888888888888",
			name:   "integration postgresql",
			engine: dbms.PostgreSQL,
			dsn:    postgresDSN,
		},
		{
			id:     "99999999-9999-4999-8999-999999999999",
			name:   "integration mysql",
			engine: dbms.MySQL,
			dsn:    mysqlDSN,
		},
	}
	for _, target := range targets {
		if target.dsn == "" {
			t.Run(string(target.engine), func(t *testing.T) { t.Skip("target DSN is not configured") })
			continue
		}
		if err := runtime.Catalog.Register(ctx, platformstore.ManagedDatabaseInput{
			ID:     target.id,
			Name:   target.name,
			Engine: target.engine,
			DSN:    security.NewSecret(target.dsn),
			Active: true,
		}); err != nil {
			t.Fatalf("register %s: %v", target.engine, err)
		}
		engine, connection, err := runtime.Connections.Connection(ctx, target.id)
		if err != nil || engine != target.engine || connection == nil {
			t.Fatalf("connect %s = (%s, %v, %v)", target.engine, engine, connection, err)
		}
	}
}
