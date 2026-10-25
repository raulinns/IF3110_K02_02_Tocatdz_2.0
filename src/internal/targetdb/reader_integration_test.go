package targetdb

import (
	"context"
	"database/sql"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	postgresadapter "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms/postgresql"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/pgtest"
)

type directResolver struct {
	engine dbms.Engine
	pool   *sql.DB
}

func (r directResolver) Connection(context.Context, security.ID) (dbms.Engine, *sql.DB, error) {
	return r.engine, r.pool, nil
}

func TestReaderAgainstPostgreSQL(t *testing.T) {
	dsn := pgtest.Database(t, "TEST_POSTGRES_TARGET_DSN")
	pool, err := postgresadapter.New().Open(context.Background(), dbms.SQLConfig{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	statements := []string{
		"DROP SCHEMA IF EXISTS viewer_test CASCADE",
		"CREATE SCHEMA viewer_test",
		"CREATE TABLE viewer_test.items (id bigint PRIMARY KEY, name text NOT NULL, payload bytea)",
		"CREATE INDEX items_name_idx ON viewer_test.items (name)",
		"INSERT INTO viewer_test.items (id, name, payload) VALUES (1, 'alpha', decode('0001', 'hex')), (2, 'beta', NULL)",
	}
	for _, statement := range statements {
		if _, err := pool.ExecContext(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
	defer pool.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS viewer_test CASCADE")

	verifyReader(t, dbms.PostgreSQL, pool, "viewer_test")
}

func verifyReader(t *testing.T, engine dbms.Engine, pool *sql.DB, schemaName string) {
	t.Helper()
	databaseID := security.ID("66666666-6666-4666-8666-666666666666")
	reader, err := NewReader(directResolver{engine: engine, pool: pool})
	if err != nil {
		t.Fatal(err)
	}

	schemas, err := reader.Schemas(context.Background(), databaseID)
	if err != nil {
		t.Fatalf("Schemas() returned error: %v", err)
	}
	found := false
	for _, schema := range schemas {
		if schema.Name == schemaName {
			for _, table := range schema.Tables {
				found = found || table.Name == "items"
			}
		}
	}
	if !found {
		t.Fatalf("Schemas() did not return %s.items: %+v", schemaName, schemas)
	}

	table, err := reader.Table(context.Background(), databaseID, schemaName, "items")
	if err != nil || table.Kind != explorer.KindTable || len(table.Columns) != 3 || len(table.Indexes) < 1 {
		t.Fatalf("Table() = (%+v, %v)", table, err)
	}
	rows, err := reader.Rows(context.Background(), databaseID, schemaName, "items", explorer.RowReadOptions{
		FetchLimit: 3,
	})
	if err != nil || rows.Order != explorer.OrderPrimaryKey || len(rows.Rows) != 2 || rows.Rows[0][1].Text != "alpha" {
		t.Fatalf("Rows() = (%+v, %v)", rows, err)
	}
}
