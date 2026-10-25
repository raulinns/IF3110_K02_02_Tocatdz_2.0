package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/mysqltest"
)

const (
	testDatabaseID  security.ID = "11111111-1111-4111-8111-111111111111"
	testMigrationID security.ID = "22222222-2222-4222-8222-222222222222"
	testExecutionID security.ID = "33333333-3333-4333-8333-333333333333"
)

func mysqlTargets(t *testing.T) (*Targets, string) {
	t.Helper()
	dsn := mysqltest.Database(t, "TEST_MYSQL_TARGET_DSN")
	pool, err := open(context.Background(), security.NewSecret(dsn))
	if err != nil {
		t.Fatalf("open MySQL test database: %v", err)
	}
	var schema string
	if err := pool.QueryRow("SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return &Targets{pools: map[security.ID]*sql.DB{testDatabaseID: pool}}, schema
}

func TestMySQLCheckValidatesRuntimeCapabilities(t *testing.T) {
	dsn := mysqltest.Database(t, "TEST_MYSQL_TARGET_DSN")
	ctx := context.Background()
	targets := &Targets{}
	if err := targets.Check(ctx, security.NewSecret(dsn)); err != nil {
		t.Fatalf("ready target rejected: %v", err)
	}

	root, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var receipts int
	if err := root.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, receiptTable).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("readiness check did not provision receipt storage: count=%d err=%v", receipts, err)
	}

	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	quotedSchema, err := quoteIdentifier(cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP USER IF EXISTS 'tocatdz_readonly'@'%'",
		"CREATE USER 'tocatdz_readonly'@'%' IDENTIFIED BY 'tocatdz-readonly-password'",
		"GRANT SELECT ON " + quotedSchema + ".* TO 'tocatdz_readonly'@'%'",
	} {
		if _, err := root.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = root.Exec("DROP USER IF EXISTS 'tocatdz_readonly'@'%'") })
	cfg.User, cfg.Passwd = "tocatdz_readonly", "tocatdz-readonly-password"
	if err := targets.Check(ctx, security.NewSecret(cfg.FormatDSN())); !errors.Is(err, security.ErrTargetNotReady) {
		t.Fatalf("read-only target accepted: %v", err)
	}
}

func TestMySQLViewerAndMigration(t *testing.T) {
	targets, schema := mysqlTargets(t)
	ctx := context.Background()
	pool := targets.pools[testDatabaseID]
	for _, statement := range []string{
		"DROP TABLE IF EXISTS `mysql_bonus_people`",
		"DROP TABLE IF EXISTS `mysql_bonus_migrated`",
		"DROP TABLE IF EXISTS `" + receiptTable + "`",
		"CREATE TABLE `mysql_bonus_people` (`id` BIGINT PRIMARY KEY, `name` VARCHAR(100), `payload` BLOB)",
		"INSERT INTO `mysql_bonus_people` (`id`, `name`, `payload`) VALUES (2, 'Budi', X'0102'), (1, 'Siti', NULL)",
	} {
		if _, err := pool.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec("DROP TABLE IF EXISTS `mysql_bonus_people`")
		_, _ = pool.Exec("DROP TABLE IF EXISTS `mysql_bonus_migrated`")
		_, _ = pool.Exec("DROP TABLE IF EXISTS `" + receiptTable + "`")
	})

	schemas, err := targets.Schemas(ctx, testDatabaseID)
	if err != nil || len(schemas) != 1 || schemas[0].Name != schema {
		t.Fatalf("Schemas() = %+v, %v", schemas, err)
	}
	table, err := targets.Table(ctx, testDatabaseID, schema, "mysql_bonus_people")
	if err != nil || table.Kind != explorer.KindTable || len(table.Columns) != 3 || len(table.Indexes) == 0 {
		t.Fatalf("Table() = %+v, %v", table, err)
	}
	batch, err := targets.Rows(ctx, testDatabaseID, schema, "mysql_bonus_people", explorer.RowReadOptions{
		Offset: 0, FetchLimit: 10, MaxCellBytes: explorer.MaxCellBytes, MaxPayloadBytes: explorer.MaxPayloadBytes,
	})
	if err != nil || batch.Order != explorer.OrderPrimaryKey || len(batch.Rows) != 2 || batch.Rows[0][0].Text != "1" || !batch.Rows[0][2].Null {
		t.Fatalf("Rows() = %+v, %v", batch, err)
	}
	if _, err := targets.Rows(ctx, testDatabaseID, "mysql", "user", explorer.RowReadOptions{
		Offset: 0, FetchLimit: 10, MaxCellBytes: explorer.MaxCellBytes, MaxPayloadBytes: explorer.MaxPayloadBytes,
	}); !errors.Is(err, security.ErrNotFound) {
		t.Fatalf("Rows() allowed cross-schema access: %v", err)
	}

	checksum := sha256.Sum256([]byte("mysql migration"))
	key := migration.ExecutionKey{DatabaseID: testDatabaseID, MigrationID: testMigrationID, ExecutionID: testExecutionID, Version: 1, Checksum: checksum}
	err = targets.WithDatabaseLock(ctx, testDatabaseID, func(ctx context.Context, locked migration.LockedTarget) error {
		journal, ok := locked.(migration.JournaledTarget)
		if !ok {
			t.Fatal("MySQL target does not implement execution journaling")
		}
		if err := journal.StartReceipt(ctx, migration.Receipt{Key: key}); err != nil {
			return err
		}
		lookup, err := locked.LookupReceipt(ctx, key)
		if err != nil || !lookup.Started || lookup.Receipt != nil {
			t.Fatalf("started receipt lookup = %+v, %v", lookup, err)
		}
		tx, err := locked.Begin(ctx)
		if err != nil {
			return err
		}
		if err := tx.Execute(ctx, security.NewSecret("CREATE TABLE `mysql_bonus_migrated` (`id` BIGINT PRIMARY KEY); INSERT INTO `mysql_bonus_migrated` VALUES (1);")); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.WriteReceipt(ctx, migration.Receipt{Key: key}); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	})
	if err != nil {
		t.Fatalf("migration execution failed: %v", err)
	}
	err = targets.WithDatabaseLock(ctx, testDatabaseID, func(ctx context.Context, locked migration.LockedTarget) error {
		lookup, err := locked.LookupReceipt(ctx, key)
		if err != nil || !lookup.Quiescent || lookup.Receipt == nil || lookup.Receipt.Key != key {
			t.Fatalf("LookupReceipt() = %+v, %v", lookup, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM `mysql_bonus_migrated`").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration result count = %d, %v", count, err)
	}
}
