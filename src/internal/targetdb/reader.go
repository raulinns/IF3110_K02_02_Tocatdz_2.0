package targetdb

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Reader struct{ connections ConnectionResolver }

var _ explorer.Reader = (*Reader)(nil)

func NewReader(connections ConnectionResolver) (*Reader, error) {
	if connections == nil {
		return nil, security.ErrInvalidInput
	}
	return &Reader{connections: connections}, nil
}

func quoteIdentifier(engine dbms.Engine, value string) (string, error) {
	if value == "" || len(value) > 63 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", security.ErrInvalidInput
	}
	switch engine {
	case dbms.PostgreSQL:
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`, nil
	default:
		return "", security.ErrUnavailable
	}
}

func readerError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return security.ErrUnavailable
}

func tableKind(engine dbms.Engine, raw string) (explorer.TableKind, error) {
	if engine != dbms.PostgreSQL {
		return "", security.ErrUnavailable
	}
	switch raw {
	case "r":
		return explorer.KindTable, nil
	case "p":
		return explorer.KindPartitionedTable, nil
	case "v":
		return explorer.KindView, nil
	case "m":
		return explorer.KindMaterializedView, nil
	}
	return "", security.ErrUnavailable
}

func (r *Reader) Schemas(ctx context.Context, databaseID security.ID) ([]explorer.Schema, error) {
	engine, pool, err := r.connections.Connection(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	var query string
	switch engine {
	case dbms.PostgreSQL:
		query = `
SELECT n.nspname, c.relname, c.relkind::text
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p', 'v', 'm')
  AND n.nspname <> 'information_schema'
  AND n.nspname NOT LIKE 'pg\_%' ESCAPE '\'
ORDER BY n.nspname, c.relname
LIMIT 10001`
	default:
		return nil, security.ErrUnavailable
	}
	rows, err := pool.QueryContext(ctx, query)
	if err != nil {
		return nil, readerError(err)
	}
	defer rows.Close()

	byName := make(map[string]int)
	result := make([]explorer.Schema, 0)
	items := 0
	for rows.Next() {
		var schemaName, tableName, rawKind string
		if err := rows.Scan(&schemaName, &tableName, &rawKind); err != nil {
			return nil, readerError(err)
		}
		kind, err := tableKind(engine, rawKind)
		if err != nil {
			return nil, err
		}
		index, exists := byName[schemaName]
		if !exists {
			index = len(result)
			byName[schemaName] = index
			result = append(result, explorer.Schema{Name: schemaName, Tables: []explorer.TableSummary{}})
		}
		result[index].Tables = append(result[index].Tables, explorer.TableSummary{Name: tableName, Kind: kind})
		items++
		if items > explorer.MaxCatalogItems {
			return nil, security.ErrResultTooLarge
		}
	}
	if err := rows.Err(); err != nil {
		return nil, readerError(err)
	}
	return result, nil
}

func (r *Reader) Table(
	ctx context.Context,
	databaseID security.ID,
	schemaName string,
	tableName string,
) (explorer.Table, error) {
	engine, pool, err := r.connections.Connection(ctx, databaseID)
	if err != nil {
		return explorer.Table{}, err
	}
	if engine != dbms.PostgreSQL {
		return explorer.Table{}, security.ErrUnavailable
	}
	result := explorer.Table{Schema: schemaName, Name: tableName}
	if result.Kind, err = relationKind(ctx, pool, engine, schemaName, tableName); err != nil {
		return explorer.Table{}, err
	}
	if result.Columns, err = readColumns(ctx, pool, schemaName, tableName); err != nil {
		return explorer.Table{}, err
	}
	result.Constraints, err = postgresConstraints(ctx, pool, schemaName, tableName)
	if err == nil {
		result.Indexes, err = postgresIndexes(ctx, pool, schemaName, tableName)
	}
	if err != nil {
		return explorer.Table{}, err
	}
	return result, nil
}

func relationKind(ctx context.Context, pool *sql.DB, engine dbms.Engine, schemaName, tableName string) (explorer.TableKind, error) {
	var raw string
	err := pool.QueryRowContext(ctx, `
SELECT c.relkind::text
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p', 'v', 'm')`, schemaName, tableName).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", security.ErrNotFound
	}
	if err != nil {
		return "", readerError(err)
	}
	return tableKind(engine, raw)
}

func readColumns(ctx context.Context, pool *sql.DB, schemaName, tableName string) ([]explorer.Column, error) {
	rows, err := pool.QueryContext(ctx, `
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position`, schemaName, tableName)
	if err != nil {
		return nil, readerError(err)
	}
	defer rows.Close()
	result := make([]explorer.Column, 0)
	for rows.Next() {
		var name, dataType, nullable string
		var defaultValue sql.NullString
		if err := rows.Scan(&name, &dataType, &nullable, &defaultValue); err != nil {
			return nil, readerError(err)
		}
		item := explorer.Column{Name: name, DataType: dataType, Nullable: nullable == "YES"}
		if defaultValue.Valid {
			item.Default = &defaultValue.String
		}
		result = append(result, item)
	}
	return result, readerError(rows.Err())
}

func postgresConstraints(ctx context.Context, pool *sql.DB, schemaName, tableName string) ([]explorer.Constraint, error) {
	rows, err := pool.QueryContext(ctx, `
SELECT con.conname, con.contype::text, pg_get_constraintdef(con.oid, true)
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class rel ON rel.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = rel.relnamespace
WHERE n.nspname = $1 AND rel.relname = $2
ORDER BY con.conname`, schemaName, tableName)
	if err != nil {
		return nil, readerError(err)
	}
	defer rows.Close()
	result := []explorer.Constraint{}
	for rows.Next() {
		var item explorer.Constraint
		if err := rows.Scan(&item.Name, &item.Type, &item.Definition); err != nil {
			return nil, readerError(err)
		}
		result = append(result, item)
	}
	return result, readerError(rows.Err())
}

func postgresIndexes(ctx context.Context, pool *sql.DB, schemaName, tableName string) ([]explorer.Index, error) {
	rows, err := pool.QueryContext(ctx, `
SELECT idx.relname, pg_get_indexdef(i.indexrelid), i.indisunique, i.indisprimary
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class rel ON rel.oid = i.indrelid
JOIN pg_catalog.pg_class idx ON idx.oid = i.indexrelid
JOIN pg_catalog.pg_namespace n ON n.oid = rel.relnamespace
WHERE n.nspname = $1 AND rel.relname = $2
ORDER BY idx.relname`, schemaName, tableName)
	if err != nil {
		return nil, readerError(err)
	}
	defer rows.Close()
	result := []explorer.Index{}
	for rows.Next() {
		var item explorer.Index
		if err := rows.Scan(&item.Name, &item.Definition, &item.Unique, &item.Primary); err != nil {
			return nil, readerError(err)
		}
		result = append(result, item)
	}
	return result, readerError(rows.Err())
}

func primaryKeyColumns(ctx context.Context, tx *sql.Tx, schemaName, tableName string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT a.attname
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class rel ON rel.oid = i.indrelid
JOIN pg_catalog.pg_namespace n ON n.oid = rel.relnamespace
JOIN pg_catalog.pg_attribute a ON a.attrelid = rel.oid AND a.attnum = ANY(i.indkey)
WHERE n.nspname = $1 AND rel.relname = $2 AND i.indisprimary
ORDER BY array_position(i.indkey, a.attnum)`, schemaName, tableName)
	if err != nil {
		return nil, readerError(err)
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, readerError(err)
		}
		result = append(result, name)
	}
	return result, readerError(rows.Err())
}

func (r *Reader) Rows(
	ctx context.Context,
	databaseID security.ID,
	schemaName string,
	tableName string,
	options explorer.RowReadOptions,
) (explorer.RowBatch, error) {
	if options.Offset < 0 || options.FetchLimit < 1 || options.FetchLimit > explorer.MaxLimit+1 {
		return explorer.RowBatch{}, security.ErrInvalidInput
	}
	engine, pool, err := r.connections.Connection(ctx, databaseID)
	if err != nil {
		return explorer.RowBatch{}, err
	}
	quotedSchema, err := quoteIdentifier(engine, schemaName)
	if err != nil {
		return explorer.RowBatch{}, err
	}
	quotedTable, err := quoteIdentifier(engine, tableName)
	if err != nil {
		return explorer.RowBatch{}, err
	}

	tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return explorer.RowBatch{}, readerError(err)
	}
	defer func() { _ = tx.Rollback() }()

	primaryKey, err := primaryKeyColumns(ctx, tx, schemaName, tableName)
	if err != nil {
		return explorer.RowBatch{}, err
	}
	order := explorer.OrderBestEffort
	orderClause := ""
	if len(primaryKey) > 0 {
		quoted := make([]string, 0, len(primaryKey))
		for _, column := range primaryKey {
			name, err := quoteIdentifier(engine, column)
			if err != nil {
				return explorer.RowBatch{}, err
			}
			quoted = append(quoted, name)
		}
		orderClause = " ORDER BY " + strings.Join(quoted, ", ")
		order = explorer.OrderPrimaryKey
	}

	query := "SELECT * FROM " + quotedSchema + "." + quotedTable + orderClause + " LIMIT $1 OFFSET $2"
	rows, err := tx.QueryContext(ctx, query, options.FetchLimit, options.Offset)
	if err != nil {
		return explorer.RowBatch{}, readerError(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return explorer.RowBatch{}, readerError(err)
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return explorer.RowBatch{}, readerError(err)
	}
	result := explorer.RowBatch{Columns: columns, Rows: [][]explorer.Cell{}, Order: order}
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return explorer.RowBatch{}, readerError(err)
		}
		row := make([]explorer.Cell, len(columns))
		for i, value := range values {
			row[i], err = cellFromValue(value, types[i].DatabaseTypeName())
			if err != nil {
				return explorer.RowBatch{}, err
			}
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return explorer.RowBatch{}, readerError(err)
	}
	if err := tx.Commit(); err != nil {
		return explorer.RowBatch{}, readerError(err)
	}
	return result, nil
}

func cellFromValue(value any, databaseType string) (explorer.Cell, error) {
	if value == nil {
		return explorer.Cell{Null: true}, nil
	}
	switch typed := value.(type) {
	case []byte:
		if strings.EqualFold(databaseType, "BYTEA") {
			return explorer.PreviewBinary(typed), nil
		}
		return explorer.PreviewText(string(typed))
	case string:
		return explorer.PreviewText(typed)
	case time.Time:
		return explorer.PreviewText(typed.UTC().Format(time.RFC3339Nano))
	case bool:
		return explorer.PreviewText(strconv.FormatBool(typed))
	case int64:
		return explorer.PreviewText(strconv.FormatInt(typed, 10))
	case float64:
		return explorer.PreviewText(strconv.FormatFloat(typed, 'g', -1, 64))
	default:
		if bytes, ok := typed.([16]byte); ok {
			return explorer.PreviewText(hex.EncodeToString(bytes[:]))
		}
		return explorer.PreviewText(fmt.Sprint(typed))
	}
}
