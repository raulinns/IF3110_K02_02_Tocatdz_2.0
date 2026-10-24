package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	targetLockKey      = 7215004403
	migrationAppName   = "tocatdz-migration"
	readStatementLimit = "4s"
	execStatementLimit = "15s"
	targetMaxConns     = 4
	maxDSNBytes        = 4 << 10
)

// Targets mengelola pool per database target. DSN hanya didekripsi saat pool dibuat dan tidak pernah dicatat.
type Targets struct {
	platform *pgxpool.Pool
	box      *SecretBox
	mu       sync.Mutex
	pools    map[security.ID]*pgxpool.Pool
}

var (
	_ explorer.Reader       = (*Targets)(nil)
	_ migration.Coordinator = (*Targets)(nil)
)

func NewTargets(platform *pgxpool.Pool, box *SecretBox) (*Targets, error) {
	if platform == nil || box == nil {
		return nil, security.ErrInvalidInput
	}
	return &Targets{platform: platform, box: box, pools: map[security.ID]*pgxpool.Pool{}}, nil
}

func targetConfig(dsn security.Secret) (*pgxpool.Config, error) {
	if dsn.Empty() || len(dsn.Reveal()) > maxDSNBytes {
		return nil, security.ErrInvalidInput
	}
	cfg, err := pgxpool.ParseConfig(dsn.Reveal())
	if err != nil {
		return nil, security.ErrInvalidInput
	}
	cfg.MaxConns = targetMaxConns
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	cfg.ConnConfig.RuntimeParams["application_name"] = "tocatdz-target"
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	return cfg, nil
}

// Check dipakai saat admin menyimpan koneksi baru: DSN harus valid dan bisa dihubungi.
func (t *Targets) Check(ctx context.Context, dsn security.Secret) error {
	cfg, err := targetConfig(dsn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		return errors.Join(security.ErrInvalidInput, errors.New("target database unreachable"))
	}
	defer conn.Close(context.WithoutCancel(ctx))
	if err := conn.Ping(ctx); err != nil {
		return errors.Join(security.ErrInvalidInput, errors.New("target database unreachable"))
	}
	return nil
}

func (t *Targets) pool(ctx context.Context, id security.ID) (*pgxpool.Pool, error) {
	if !security.ValidID(id) {
		return nil, security.ErrNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if pool, ok := t.pools[id]; ok {
		return pool, nil
	}
	var sealed []byte
	if err := t.platform.QueryRow(ctx, "SELECT dsn_ciphertext FROM managed_databases WHERE id = $1", string(id)).Scan(&sealed); err != nil {
		return nil, mapError(err)
	}
	dsn, err := t.box.Open(id, sealed)
	if err != nil {
		return nil, security.ErrUnavailable
	}
	cfg, err := targetConfig(dsn)
	if err != nil {
		return nil, security.ErrUnavailable
	}
	pool, err := pgxpool.NewWithConfig(context.WithoutCancel(ctx), cfg)
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
		pool.Close()
		delete(t.pools, id)
	}
}

// Kegagalan koneksi ke target selalu ErrUnavailable; teks error driver tidak diteruskan.
func targetError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	mapped := mapError(err)
	if errors.Is(mapped, security.ErrNotFound) || errors.Is(mapped, security.ErrResultTooLarge) {
		return mapped
	}
	return security.ErrUnavailable
}

func (t *Targets) read(ctx context.Context, id security.ID, run func(pgx.Tx) error) error {
	pool, err := t.pool(ctx, id)
	if err != nil {
		return err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return targetError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '"+readStatementLimit+"'"); err != nil {
		return targetError(ctx, err)
	}
	return targetError(ctx, run(tx))
}

var tableKinds = map[string]explorer.TableKind{
	"r": explorer.KindTable, "p": explorer.KindPartitionedTable, "v": explorer.KindView, "m": explorer.KindMaterializedView,
}

const hiddenSchemas = `n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'tocatdz_internal'`

func (t *Targets) Schemas(ctx context.Context, id security.ID) ([]explorer.Schema, error) {
	var result []explorer.Schema
	err := t.read(ctx, id, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT n.nspname, c.relname, c.relkind::text FROM pg_namespace n
			LEFT JOIN pg_class c ON c.relnamespace = n.oid AND c.relkind IN ('r', 'p', 'v', 'm')
			WHERE `+hiddenSchemas+` ORDER BY n.nspname, c.relname LIMIT $1`, explorer.MaxCatalogItems+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		index := map[string]int{}
		for rows.Next() {
			var schema string
			var table, kind *string
			if err := rows.Scan(&schema, &table, &kind); err != nil {
				return err
			}
			position, ok := index[schema]
			if !ok {
				position = len(result)
				index[schema] = position
				result = append(result, explorer.Schema{Name: schema, Tables: []explorer.TableSummary{}})
			}
			if table != nil && kind != nil {
				result[position].Tables = append(result[position].Tables, explorer.TableSummary{Name: *table, Kind: tableKinds[*kind]})
			}
		}
		return rows.Err()
	})
	return result, err
}

func relation(ctx context.Context, tx pgx.Tx, schema, table string) (uint32, explorer.TableKind, error) {
	var oid uint32
	var kind string
	err := tx.QueryRow(ctx, `SELECT c.oid, c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p', 'v', 'm') AND `+hiddenSchemas, schema, table).Scan(&oid, &kind)
	if err != nil {
		return 0, "", mapError(err)
	}
	return oid, tableKinds[kind], nil
}

var constraintTypes = map[string]string{
	"p": "PRIMARY KEY", "f": "FOREIGN KEY", "u": "UNIQUE", "c": "CHECK", "x": "EXCLUDE", "n": "NOT NULL", "t": "TRIGGER",
}

func (t *Targets) Table(ctx context.Context, id security.ID, schema, name string) (explorer.Table, error) {
	result := explorer.Table{Schema: schema, Name: name}
	err := t.read(ctx, id, func(tx pgx.Tx) error {
		oid, kind, err := relation(ctx, tx, schema, name)
		if err != nil {
			return err
		}
		result.Kind = kind
		rows, err := tx.Query(ctx, `SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull, pg_get_expr(d.adbin, d.adrelid)
			FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			WHERE a.attrelid = $1 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, oid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var column explorer.Column
			if err := rows.Scan(&column.Name, &column.DataType, &column.Nullable, &column.Default); err != nil {
				rows.Close()
				return err
			}
			result.Columns = append(result.Columns, column)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, "SELECT conname, contype::text, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = $1 ORDER BY conname", oid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var constraint explorer.Constraint
			var kind string
			if err := rows.Scan(&constraint.Name, &kind, &constraint.Definition); err != nil {
				rows.Close()
				return err
			}
			constraint.Type = constraintTypes[kind]
			if constraint.Type == "" {
				constraint.Type = "OTHER"
			}
			result.Constraints = append(result.Constraints, constraint)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT i.relname, pg_get_indexdef(x.indexrelid), x.indisunique, x.indisprimary
			FROM pg_index x JOIN pg_class i ON i.oid = x.indexrelid WHERE x.indrelid = $1 ORDER BY i.relname`, oid)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var index explorer.Index
			if err := rows.Scan(&index.Name, &index.Definition, &index.Unique, &index.Primary); err != nil {
				return err
			}
			result.Indexes = append(result.Indexes, index)
		}
		return rows.Err()
	})
	return result, err
}

type rowColumn struct {
	name   string
	binary bool
}

// Rows membaca data lewat SELECT yang dibangun dari nama kolom katalog; nilai dipotong di database supaya hasil tetap kecil.
func (t *Targets) Rows(ctx context.Context, id security.ID, schema, table string, options explorer.RowReadOptions) (explorer.RowBatch, error) {
	var batch explorer.RowBatch
	if options.FetchLimit < 1 || options.Offset < 0 || options.MaxCellBytes < 4 || options.MaxPayloadBytes < 1 {
		return batch, security.ErrInvalidInput
	}
	err := t.read(ctx, id, func(tx pgx.Tx) error {
		oid, _, err := relation(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT a.attname, a.atttypid = 'bytea'::regtype FROM pg_attribute a
			WHERE a.attrelid = $1 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, oid)
		if err != nil {
			return err
		}
		var columns []rowColumn
		for rows.Next() {
			var column rowColumn
			if err := rows.Scan(&column.name, &column.binary); err != nil {
				rows.Close()
				return err
			}
			columns = append(columns, column)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(columns) > explorer.MaxColumns {
			return security.ErrResultTooLarge
		}
		var keys []string
		rows, err = tx.Query(ctx, `SELECT a.attname FROM pg_index x JOIN LATERAL unnest(x.indkey) WITH ORDINALITY AS k(attnum, position) ON true
			JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = k.attnum WHERE x.indrelid = $1 AND x.indisprimary ORDER BY k.position`, oid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, key)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		relationName, err := quoted(schema, table)
		if err != nil {
			return err
		}
		textLimit := strconv.Itoa(options.MaxCellBytes + 1)
		binaryLimit := strconv.Itoa((options.MaxCellBytes-2)/2 + 1)
		selects := make([]string, len(columns))
		for i, column := range columns {
			name, err := explorer.QuoteIdentifier(column.name)
			if err != nil {
				return security.ErrUnavailable
			}
			if column.binary {
				selects[i] = "substring(" + name + " FROM 1 FOR " + binaryLimit + ")"
			} else {
				selects[i] = "left(" + name + "::text, " + textLimit + ")"
			}
			batch.Columns = append(batch.Columns, column.name)
		}
		query := "SELECT " + strings.Join(selects, ", ") + " FROM " + relationName
		batch.Order = explorer.OrderBestEffort
		if len(keys) > 0 {
			ordered := make([]string, len(keys))
			for i, key := range keys {
				if ordered[i], err = explorer.QuoteIdentifier(key); err != nil {
					return security.ErrUnavailable
				}
			}
			query += " ORDER BY " + strings.Join(ordered, ", ")
			batch.Order = explorer.OrderPrimaryKey
		}
		query += " LIMIT $1 OFFSET $2"
		data, err := tx.Query(ctx, query, options.FetchLimit, options.Offset)
		if err != nil {
			return err
		}
		defer data.Close()
		used := 0
		for data.Next() {
			targets := make([]any, len(columns))
			texts := make([]*string, len(columns))
			binaries := make([][]byte, len(columns))
			for i, column := range columns {
				if column.binary {
					targets[i] = &binaries[i]
				} else {
					targets[i] = &texts[i]
				}
			}
			if err := data.Scan(targets...); err != nil {
				return err
			}
			row := make([]explorer.Cell, len(columns))
			for i, column := range columns {
				switch {
				case column.binary && binaries[i] == nil, !column.binary && texts[i] == nil:
					row[i] = explorer.Cell{Null: true}
				case column.binary:
					row[i] = explorer.PreviewBinary(binaries[i])
				default:
					cell, err := explorer.PreviewText(*texts[i])
					if err != nil {
						return security.ErrUnavailable
					}
					row[i] = cell
				}
				used += len(row[i].Text) + 16
			}
			if used > options.MaxPayloadBytes {
				return security.ErrResultTooLarge
			}
			batch.Rows = append(batch.Rows, row)
		}
		return data.Err()
	})
	return batch, err
}

func quoted(schema, table string) (string, error) {
	s, err := explorer.QuoteIdentifier(schema)
	if err != nil {
		return "", security.ErrInvalidInput
	}
	t, err := explorer.QuoteIdentifier(table)
	if err != nil {
		return "", security.ErrInvalidInput
	}
	return s + "." + t, nil
}

// Lock diambil di database target itu sendiri, jadi dua entri koneksi yang menunjuk database fisik yang sama tetap saling menunggu.
func (t *Targets) WithDatabaseLock(ctx context.Context, id security.ID, run func(context.Context, migration.LockedTarget) error) error {
	pool, err := t.pool(ctx, id)
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return targetError(ctx, err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(targetLockKey)); err != nil {
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
		return targetError(ctx, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), migration.CleanupTimeout)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock($1)", int64(targetLockKey)); err != nil {
			// Menutup koneksi melepas session lock walaupun unlock gagal.
			_ = conn.Conn().Close(cleanup)
		}
	}()
	return run(ctx, &lockedTarget{pool: pool})
}

type lockedTarget struct{ pool *pgxpool.Pool }

func (l *lockedTarget) Begin(ctx context.Context) (migration.Transaction, error) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return nil, targetError(ctx, err)
	}
	for _, statement := range []string{
		"SET LOCAL application_name = '" + migrationAppName + "'",
		"SET LOCAL statement_timeout = '" + execStatementLimit + "'",
		"SET LOCAL lock_timeout = '5s'",
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return nil, targetError(ctx, err)
		}
	}
	return &targetTx{tx: tx}, nil
}

const receiptTable = "tocatdz_internal.migration_receipts"

// Quiescent hanya benar jika tidak ada transaksi migration lain yang masih hidup di target; kalau ada, hasilnya belum bisa dipastikan.
func (l *lockedTarget) LookupReceipt(ctx context.Context, key migration.ExecutionKey) (migration.ReceiptLookup, error) {
	var lookup migration.ReceiptLookup
	var busy int
	if err := l.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
		AND application_name = $1 AND state <> 'idle' AND pid <> pg_backend_pid()`, migrationAppName).Scan(&busy); err != nil {
		return lookup, targetError(ctx, err)
	}
	lookup.Quiescent = busy == 0
	var exists bool
	if err := l.pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", receiptTable).Scan(&exists); err != nil {
		return lookup, targetError(ctx, err)
	}
	if !exists {
		return lookup, nil
	}
	var database, migrationID, execution string
	var version int64
	var checksum []byte
	err := l.pool.QueryRow(ctx, "SELECT database_id::text, migration_id::text, execution_id::text, version, checksum FROM "+receiptTable+" WHERE execution_id = $1",
		string(key.ExecutionID)).Scan(&database, &migrationID, &execution, &version, &checksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return lookup, nil
	}
	if err != nil || len(checksum) != 32 {
		return lookup, security.ErrUnavailable
	}
	receipt := migration.Receipt{Key: migration.ExecutionKey{DatabaseID: security.ID(database), MigrationID: security.ID(migrationID),
		ExecutionID: security.ID(execution), Version: version, Checksum: [32]byte(checksum)}}
	lookup.Receipt = &receipt
	return lookup, nil
}

type targetTx struct{ tx pgx.Tx }

// SQL migration dikirim tanpa parameter sehingga pgx memakai simple protocol dan beberapa statement bisa jalan dalam satu transaksi.
func (t *targetTx) Execute(ctx context.Context, sql security.Secret) error {
	if _, err := t.tx.Exec(ctx, sql.Reveal()); err != nil {
		return errors.New("migration statement failed")
	}
	return nil
}

func (t *targetTx) WriteReceipt(ctx context.Context, receipt migration.Receipt) error {
	key := receipt.Key
	for _, statement := range []string{
		"CREATE SCHEMA IF NOT EXISTS tocatdz_internal",
		`CREATE TABLE IF NOT EXISTS ` + receiptTable + ` (execution_id uuid PRIMARY KEY, database_id uuid NOT NULL, migration_id uuid NOT NULL,
			version bigint NOT NULL, checksum bytea NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now())`,
	} {
		if _, err := t.tx.Exec(ctx, statement); err != nil {
			return security.ErrUnavailable
		}
	}
	_, err := t.tx.Exec(ctx, "INSERT INTO "+receiptTable+" (execution_id, database_id, migration_id, version, checksum) VALUES ($1, $2, $3, $4, $5)",
		string(key.ExecutionID), string(key.DatabaseID), string(key.MigrationID), key.Version, key.Checksum[:])
	if err != nil {
		return security.ErrUnavailable
	}
	return nil
}

func (t *targetTx) Commit(ctx context.Context) error {
	if err := t.tx.Commit(ctx); err != nil {
		return errors.New("migration commit failed")
	}
	return nil
}

func (t *targetTx) Rollback(ctx context.Context) error {
	if err := t.tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return errors.New("migration rollback failed")
	}
	return nil
}
