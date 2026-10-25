package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	connectTimeout = 5 * time.Second
	checkTimeout   = 10 * time.Second
	lockName       = "tocatdz:migration"
	receiptTable   = "_tocatdz_migration_receipts"
	maxDSNBytes    = 4 << 10
)

type secretOpener interface {
	Open(security.ID, []byte) (security.Secret, error)
}

// Targets mengelola pool MySQL target. PostgreSQL platform tetap menjadi sumber metadata terenkripsi.
type Targets struct {
	platform *pgxpool.Pool
	box      secretOpener
	mu       sync.Mutex
	pools    map[security.ID]*sql.DB
}

var (
	_ explorer.Reader           = (*Targets)(nil)
	_ migration.Coordinator     = (*Targets)(nil)
	_ migration.JournaledTarget = (*lockedTarget)(nil)
)

func NewTargets(platform *pgxpool.Pool, box secretOpener) (*Targets, error) {
	if platform == nil || box == nil {
		return nil, security.ErrInvalidInput
	}
	return &Targets{platform: platform, box: box, pools: make(map[security.ID]*sql.DB)}, nil
}

func targetConfig(dsn security.Secret) (*mysqldriver.Config, error) {
	if dsn.Empty() || len(dsn.Reveal()) > maxDSNBytes {
		return nil, security.ErrInvalidInput
	}
	cfg, err := mysqldriver.ParseDSN(dsn.Reveal())
	if err != nil || strings.TrimSpace(cfg.DBName) == "" {
		return nil, security.ErrInvalidInput
	}
	cfg.Timeout = connectTimeout
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 20 * time.Second
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = true
	return cfg, nil
}

func open(ctx context.Context, dsn security.Secret) (*sql.DB, error) {
	cfg, err := targetConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, security.ErrInvalidInput
	}
	pool.SetMaxOpenConns(dbms.DefaultMaxOpenConns)
	pool.SetMaxIdleConns(dbms.DefaultMaxIdleConns)
	pool.SetConnMaxLifetime(dbms.DefaultConnMaxLifetime)
	pool.SetConnMaxIdleTime(dbms.DefaultConnMaxIdleTime)
	check, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.PingContext(check); err != nil {
		_ = pool.Close()
		return nil, errors.Join(security.ErrInvalidInput, errors.New("target database unreachable"))
	}
	return pool, nil
}

func (t *Targets) Check(ctx context.Context, dsn security.Secret) error {
	pool, err := open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	check, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	conn, err := pool.Conn(check)
	if err != nil {
		return readinessError(check)
	}
	defer conn.Close()
	if err := checkReady(check, conn); err != nil {
		return readinessError(check)
	}
	return nil
}

func readinessError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return security.ErrTargetNotReady
}

func checkReady(ctx context.Context, conn *sql.Conn) error {
	var schema string
	var catalogItems int
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil || schema == "" {
		return security.ErrTargetNotReady
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE()`).Scan(&catalogItems); err != nil {
		return security.ErrTargetNotReady
	}

	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", lockName, 3).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return security.ErrTargetNotReady
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), migration.CleanupTimeout)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, "SELECT RELEASE_LOCK(?)", lockName)
	}()
	if err := ensureReceipts(ctx, conn); err != nil {
		return security.ErrTargetNotReady
	}
	return probeReceiptWrites(ctx, conn)
}

func probeReceiptWrites(ctx context.Context, conn *sql.Conn) error {
	ids := security.NewTokenGenerator()
	executionID, err := ids.GenerateID()
	if err != nil {
		return security.ErrUnavailable
	}
	databaseID, err := ids.GenerateID()
	if err != nil {
		return security.ErrUnavailable
	}
	migrationID, err := ids.GenerateID()
	if err != nil {
		return security.ErrUnavailable
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return security.ErrTargetNotReady
	}
	defer tx.Rollback()
	checksum := make([]byte, 32)
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+receiptTable+`
		(execution_id, database_id, migration_id, version, checksum, status)
		VALUES (?, ?, ?, 1, ?, 'started')`, string(executionID), string(databaseID), string(migrationID), checksum); err != nil {
		return security.ErrTargetNotReady
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+receiptTable+`
		SET status = 'applied', applied_at = CURRENT_TIMESTAMP(6)
		WHERE execution_id = ? AND status = 'started'`, string(executionID))
	if err != nil {
		return security.ErrTargetNotReady
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return security.ErrTargetNotReady
	}
	return nil
}

func (t *Targets) pool(ctx context.Context, id security.ID) (*sql.DB, error) {
	if !security.ValidID(id) {
		return nil, security.ErrNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if pool := t.pools[id]; pool != nil {
		return pool, nil
	}
	var engine string
	var sealed []byte
	if err := t.platform.QueryRow(ctx, "SELECT engine, dsn_ciphertext FROM managed_databases WHERE id = $1", string(id)).Scan(&engine, &sealed); err != nil {
		return nil, security.ErrNotFound
	}
	if engine != string(dbms.MySQL) {
		return nil, security.ErrUnavailable
	}
	dsn, err := t.box.Open(id, sealed)
	if err != nil {
		return nil, security.ErrUnavailable
	}
	pool, err := open(context.WithoutCancel(ctx), dsn)
	if err != nil {
		return nil, security.ErrUnavailable
	}
	t.pools[id] = pool
	return pool, nil
}

func (t *Targets) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, pool := range t.pools {
		_ = pool.Close()
		delete(t.pools, id)
	}
}

func targetError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, sql.ErrNoRows) {
		return security.ErrNotFound
	}
	return security.ErrUnavailable
}

func quoteIdentifier(value string) (string, error) {
	if value == "" || len(value) > 64 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", security.ErrInvalidInput
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`", nil
}

func currentSchema(ctx context.Context, pool *sql.DB) (string, error) {
	var schema string
	if err := pool.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
		return "", targetError(ctx, err)
	}
	if schema == "" {
		return "", security.ErrUnavailable
	}
	return schema, nil
}

func (t *Targets) Schemas(ctx context.Context, id security.ID) ([]explorer.Schema, error) {
	pool, err := t.pool(ctx, id)
	if err != nil {
		return nil, err
	}
	schema, err := currentSchema(ctx, pool)
	if err != nil {
		return nil, err
	}
	result := []explorer.Schema{{Name: schema, Tables: []explorer.TableSummary{}}}
	rows, err := pool.QueryContext(ctx, `SELECT TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME <> ? ORDER BY TABLE_NAME LIMIT ?`, receiptTable, explorer.MaxCatalogItems+1)
	if err != nil {
		return nil, targetError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, rawKind string
		if err := rows.Scan(&name, &rawKind); err != nil {
			return nil, targetError(ctx, err)
		}
		kind := explorer.KindTable
		if rawKind == "VIEW" {
			kind = explorer.KindView
		}
		result[0].Tables = append(result[0].Tables, explorer.TableSummary{Name: name, Kind: kind})
		if len(result[0].Tables) > explorer.MaxCatalogItems {
			return nil, security.ErrResultTooLarge
		}
	}
	return result, targetError(ctx, rows.Err())
}

func (t *Targets) Table(ctx context.Context, id security.ID, schema, name string) (explorer.Table, error) {
	pool, err := t.pool(ctx, id)
	if err != nil {
		return explorer.Table{}, err
	}
	result := explorer.Table{Schema: schema, Name: name}
	var kind string
	if err := pool.QueryRowContext(ctx, `SELECT TABLE_TYPE FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_SCHEMA = ? AND TABLE_NAME = ? AND TABLE_NAME <> ?`, schema, name, receiptTable).Scan(&kind); err != nil {
		return explorer.Table{}, targetError(ctx, err)
	}
	result.Kind = explorer.KindTable
	if kind == "VIEW" {
		result.Kind = explorer.KindView
	}
	rows, err := pool.QueryContext(ctx, `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, schema, name)
	if err != nil {
		return explorer.Table{}, targetError(ctx, err)
	}
	for rows.Next() {
		var column explorer.Column
		var nullable string
		var defaultValue sql.NullString
		if err := rows.Scan(&column.Name, &column.DataType, &nullable, &defaultValue); err != nil {
			rows.Close()
			return explorer.Table{}, targetError(ctx, err)
		}
		column.Nullable = nullable == "YES"
		if defaultValue.Valid {
			column.Default = &defaultValue.String
		}
		result.Columns = append(result.Columns, column)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return explorer.Table{}, targetError(ctx, err)
	}
	rows, err = pool.QueryContext(ctx, `SELECT tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE,
		COALESCE(GROUP_CONCAT(kcu.COLUMN_NAME ORDER BY kcu.ORDINAL_POSITION SEPARATOR ', '), '')
		FROM information_schema.TABLE_CONSTRAINTS tc LEFT JOIN information_schema.KEY_COLUMN_USAGE kcu
		ON kcu.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA AND kcu.TABLE_NAME = tc.TABLE_NAME AND kcu.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
		WHERE tc.TABLE_SCHEMA = ? AND tc.TABLE_NAME = ? GROUP BY tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE ORDER BY tc.CONSTRAINT_NAME`, schema, name)
	if err != nil {
		return explorer.Table{}, targetError(ctx, err)
	}
	for rows.Next() {
		var item explorer.Constraint
		var columns string
		if err := rows.Scan(&item.Name, &item.Type, &columns); err != nil {
			rows.Close()
			return explorer.Table{}, targetError(ctx, err)
		}
		item.Definition = item.Type + " (" + columns + ")"
		result.Constraints = append(result.Constraints, item)
	}
	rows.Close()
	rows, err = pool.QueryContext(ctx, `SELECT INDEX_NAME, NON_UNIQUE = 0, INDEX_NAME = 'PRIMARY',
		GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ', ')
		FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		GROUP BY INDEX_NAME, NON_UNIQUE ORDER BY INDEX_NAME`, schema, name)
	if err != nil {
		return explorer.Table{}, targetError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		var item explorer.Index
		var columns string
		if err := rows.Scan(&item.Name, &item.Unique, &item.Primary, &columns); err != nil {
			return explorer.Table{}, targetError(ctx, err)
		}
		item.Definition = "INDEX (" + columns + ")"
		result.Indexes = append(result.Indexes, item)
	}
	return result, targetError(ctx, rows.Err())
}

type rowColumn struct {
	name   string
	binary bool
}

func (t *Targets) Rows(ctx context.Context, id security.ID, schema, table string, options explorer.RowReadOptions) (explorer.RowBatch, error) {
	var result explorer.RowBatch
	if options.Offset < 0 || options.FetchLimit < 1 || options.FetchLimit > explorer.MaxLimit+1 || options.MaxCellBytes < 4 || options.MaxPayloadBytes < 1 {
		return result, security.ErrInvalidInput
	}
	pool, err := t.pool(ctx, id)
	if err != nil {
		return result, err
	}
	selectedSchema, err := currentSchema(ctx, pool)
	if err != nil {
		return result, err
	}
	if schema != selectedSchema {
		return result, security.ErrNotFound
	}
	var columns []rowColumn
	rows, err := pool.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE IN ('binary','varbinary','tinyblob','blob','mediumblob','longblob','bit')
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return result, targetError(ctx, err)
	}
	for rows.Next() {
		var column rowColumn
		if err := rows.Scan(&column.name, &column.binary); err != nil {
			rows.Close()
			return result, targetError(ctx, err)
		}
		columns = append(columns, column)
	}
	rows.Close()
	if len(columns) == 0 {
		return result, security.ErrNotFound
	}
	if len(columns) > explorer.MaxColumns {
		return result, security.ErrResultTooLarge
	}
	var keys []string
	rows, err = pool.QueryContext(ctx, `SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return result, targetError(ctx, err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return result, targetError(ctx, err)
		}
		keys = append(keys, key)
	}
	rows.Close()
	quotedSchema, err := quoteIdentifier(schema)
	if err != nil {
		return result, err
	}
	quotedTable, err := quoteIdentifier(table)
	if err != nil {
		return result, err
	}
	selects := make([]string, len(columns))
	for i, column := range columns {
		quoted, err := quoteIdentifier(column.name)
		if err != nil {
			return result, security.ErrUnavailable
		}
		result.Columns = append(result.Columns, column.name)
		selects[i] = "LEFT(CAST(" + quoted + " AS CHAR), " + strconv.Itoa(options.MaxCellBytes+1) + ")"
		if column.binary {
			selects[i] = "LEFT(" + quoted + ", " + strconv.Itoa(options.MaxCellBytes+1) + ")"
		}
	}
	query := "SELECT " + strings.Join(selects, ", ") + " FROM " + quotedSchema + "." + quotedTable
	result.Order = explorer.OrderBestEffort
	if len(keys) > 0 {
		quoted := make([]string, len(keys))
		for i, key := range keys {
			quoted[i], err = quoteIdentifier(key)
			if err != nil {
				return result, security.ErrUnavailable
			}
		}
		query += " ORDER BY " + strings.Join(quoted, ", ")
		result.Order = explorer.OrderPrimaryKey
	}
	query += " LIMIT ? OFFSET ?"
	data, err := pool.QueryContext(ctx, query, options.FetchLimit, options.Offset)
	if err != nil {
		return result, targetError(ctx, err)
	}
	defer data.Close()
	used := 0
	for data.Next() {
		values := make([]sql.RawBytes, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := data.Scan(dest...); err != nil {
			return result, targetError(ctx, err)
		}
		row := make([]explorer.Cell, len(columns))
		for i, value := range values {
			if value == nil {
				row[i] = explorer.Cell{Null: true}
			} else if columns[i].binary {
				row[i] = explorer.PreviewBinary(value)
			} else {
				row[i], err = explorer.PreviewText(string(value))
				if err != nil {
					return result, security.ErrUnavailable
				}
			}
			used += len(row[i].Text) + 16
		}
		if used > options.MaxPayloadBytes {
			return result, security.ErrResultTooLarge
		}
		result.Rows = append(result.Rows, row)
	}
	return result, targetError(ctx, data.Err())
}

func ensureReceipts(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+receiptTable+` (
		execution_id CHAR(36) PRIMARY KEY, database_id CHAR(36) NOT NULL, migration_id CHAR(36) NOT NULL,
		version BIGINT NOT NULL, checksum BINARY(32) NOT NULL,
		status ENUM('started', 'applied') NOT NULL,
		started_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), applied_at TIMESTAMP(6) NULL,
		UNIQUE KEY receipt_version (database_id, version)) ENGINE=InnoDB`)
	return err
}

func (t *Targets) WithDatabaseLock(ctx context.Context, id security.ID, run func(context.Context, migration.LockedTarget) error) error {
	pool, err := t.pool(ctx, id)
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return targetError(ctx, err)
	}
	defer conn.Close()
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", lockName, 10).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return security.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), migration.CleanupTimeout)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, "SELECT RELEASE_LOCK(?)", lockName)
	}()
	if err := ensureReceipts(ctx, conn); err != nil {
		return security.ErrTargetNotReady
	}
	return run(ctx, &lockedTarget{conn: conn})
}

type lockedTarget struct{ conn *sql.Conn }

func (l *lockedTarget) StartReceipt(ctx context.Context, receipt migration.Receipt) error {
	key := receipt.Key
	_, err := l.conn.ExecContext(ctx, `INSERT INTO `+receiptTable+`
		(execution_id, database_id, migration_id, version, checksum, status)
		VALUES (?, ?, ?, ?, ?, 'started')`, string(key.ExecutionID), string(key.DatabaseID),
		string(key.MigrationID), key.Version, key.Checksum[:])
	if err == nil {
		return nil
	}

	var databaseID, migrationID string
	var version int64
	var checksum []byte
	if scanErr := l.conn.QueryRowContext(ctx, `SELECT database_id, migration_id, version, checksum
		FROM `+receiptTable+` WHERE execution_id = ?`, string(key.ExecutionID)).Scan(
		&databaseID, &migrationID, &version, &checksum,
	); scanErr != nil || len(checksum) != len(key.Checksum) {
		return security.ErrUnavailable
	}
	if databaseID != string(key.DatabaseID) || migrationID != string(key.MigrationID) ||
		version != key.Version || string(checksum) != string(key.Checksum[:]) {
		return security.ErrUnavailable
	}
	return nil
}

func (l *lockedTarget) Begin(ctx context.Context) (migration.Transaction, error) {
	tx, err := l.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, targetError(ctx, err)
	}
	return &targetTx{tx: tx}, nil
}

func (l *lockedTarget) LookupReceipt(ctx context.Context, key migration.ExecutionKey) (migration.ReceiptLookup, error) {
	lookup := migration.ReceiptLookup{Quiescent: true}
	var databaseID, migrationID, executionID, status string
	var version int64
	var checksum []byte
	err := l.conn.QueryRowContext(ctx, `SELECT database_id, migration_id, execution_id, version, checksum, status
		FROM `+receiptTable+` WHERE execution_id = ?`, string(key.ExecutionID)).Scan(
		&databaseID, &migrationID, &executionID, &version, &checksum, &status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return lookup, nil
	}
	if err != nil || len(checksum) != 32 || (status != "started" && status != "applied") {
		return lookup, security.ErrUnavailable
	}
	lookup.Started = true
	if status == "started" {
		return lookup, nil
	}
	var digest [32]byte
	copy(digest[:], checksum)
	lookup.Receipt = &migration.Receipt{Key: migration.ExecutionKey{DatabaseID: security.ID(databaseID), MigrationID: security.ID(migrationID), ExecutionID: security.ID(executionID), Version: version, Checksum: digest}}
	return lookup, nil
}

type targetTx struct{ tx *sql.Tx }

func (t *targetTx) Execute(ctx context.Context, statement security.Secret) error {
	if _, err := t.tx.ExecContext(ctx, statement.Reveal()); err != nil {
		return errors.New("migration statement failed")
	}
	return nil
}

func (t *targetTx) WriteReceipt(ctx context.Context, receipt migration.Receipt) error {
	key := receipt.Key
	result, err := t.tx.ExecContext(ctx, `UPDATE `+receiptTable+`
		SET status = 'applied', applied_at = CURRENT_TIMESTAMP(6)
		WHERE execution_id = ? AND database_id = ? AND migration_id = ? AND version = ?
		AND checksum = ? AND status = 'started'`, string(key.ExecutionID), string(key.DatabaseID),
		string(key.MigrationID), key.Version, key.Checksum[:])
	if err != nil {
		return security.ErrUnavailable
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return security.ErrUnavailable
	}
	return nil
}

func (t *targetTx) Commit(ctx context.Context) error {
	if err := t.tx.Commit(); err != nil {
		return fmt.Errorf("migration commit failed: %w", security.ErrUnavailable)
	}
	return nil
}

func (t *targetTx) Rollback(context.Context) error {
	if err := t.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return errors.New("migration rollback failed")
	}
	return nil
}
