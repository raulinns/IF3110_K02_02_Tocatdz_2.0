package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const maxSnapshotBytes = 8 << 20

var _ schemadiff.Reader = (*Targets)(nil)

const snapshotRelations = `c.relnamespace = $1 AND c.relkind IN ('r', 'p') AND NOT c.relispartition`

type snapshotBudget struct{ objects, bytes int }

func (b *snapshotBudget) add(texts ...string) error {
	b.objects++
	for _, text := range texts {
		b.bytes += len(text)
	}
	if b.objects > schemadiff.MaxObjects || b.bytes > maxSnapshotBytes {
		return security.ErrResultTooLarge
	}
	return nil
}

// Snapshot membaca struktur satu schema dalam transaksi baca saja. search_path hanya pg_catalog agar
// definisi dari katalog selalu menuliskan schema objek secara eksplisit.
func (t *Targets) Snapshot(ctx context.Context, id security.ID, schema string) (schemadiff.Snapshot, error) {
	result := schemadiff.Snapshot{Schema: schema, Tables: []schemadiff.Table{}, Views: []schemadiff.View{}}
	if !schemadiff.ValidSchemaName(schema) {
		return result, security.ErrInvalidInput
	}
	err := t.read(ctx, id, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL search_path = pg_catalog"); err != nil {
			return err
		}
		var namespace uint32
		if err := tx.QueryRow(ctx, `SELECT n.oid, quote_ident(n.nspname) FROM pg_namespace n WHERE n.nspname = $1 AND `+hiddenSchemas, schema).Scan(&namespace, &result.Quoted); err != nil {
			return mapError(err)
		}
		budget := &snapshotBudget{}
		tables := map[uint32]int{}
		rows, err := tx.Query(ctx, `SELECT c.oid, c.relname, quote_ident(c.relname) FROM pg_class c WHERE `+snapshotRelations+` ORDER BY c.relname LIMIT $2`, namespace, schemadiff.MaxTables+1)
		if err != nil {
			return err
		}
		for rows.Next() {
			var oid uint32
			var table schemadiff.Table
			if err := rows.Scan(&oid, &table.Name, &table.Quoted); err != nil {
				rows.Close()
				return err
			}
			tables[oid] = len(result.Tables)
			result.Tables = append(result.Tables, table)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result.Tables) > schemadiff.MaxTables {
			return security.ErrResultTooLarge
		}
		if err := snapshotColumns(ctx, tx, namespace, tables, &result, budget); err != nil {
			return err
		}
		if err := snapshotConstraints(ctx, tx, namespace, tables, &result, budget); err != nil {
			return err
		}
		if err := snapshotIndexes(ctx, tx, namespace, tables, &result, budget); err != nil {
			return err
		}
		return snapshotViews(ctx, tx, namespace, &result, budget)
	})
	return result, err
}

func snapshotColumns(ctx context.Context, tx pgx.Tx, namespace uint32, tables map[uint32]int, result *schemadiff.Snapshot, budget *snapshotBudget) error {
	rows, err := tx.Query(ctx, `SELECT a.attrelid, a.attname, quote_ident(a.attname), format_type(a.atttypid, a.atttypmod), a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid), a.attidentity::text, a.attgenerated::text,
			a.attidentity = '' AND a.attgenerated = '' AND pg_get_expr(d.adbin, d.adrelid) LIKE 'nextval(%' AND EXISTS (
				SELECT 1 FROM pg_depend dep JOIN pg_class s ON s.oid = dep.objid AND s.relkind = 'S'
				WHERE dep.classid = 'pg_class'::regclass AND dep.refobjid = a.attrelid AND dep.refobjsubid = a.attnum AND dep.deptype = 'a')
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE `+snapshotRelations+` AND a.attnum > 0 AND NOT a.attisdropped ORDER BY c.relname, a.attnum`, namespace)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var relation uint32
		var column schemadiff.Column
		if err := rows.Scan(&relation, &column.Name, &column.Quoted, &column.Type, &column.NotNull, &column.Default, &column.Identity, &column.Generated, &column.Serial); err != nil {
			return err
		}
		position, ok := tables[relation]
		if !ok {
			continue
		}
		expression := ""
		if column.Default != nil {
			expression = *column.Default
		}
		if err := budget.add(column.Name, column.Type, expression); err != nil {
			return err
		}
		result.Tables[position].Columns = append(result.Tables[position].Columns, column)
	}
	return rows.Err()
}

func snapshotConstraints(ctx context.Context, tx pgx.Tx, namespace uint32, tables map[uint32]int, result *schemadiff.Snapshot, budget *snapshotBudget) error {
	rows, err := tx.Query(ctx, `SELECT k.conrelid, k.conname, quote_ident(k.conname), k.contype::text, pg_get_constraintdef(k.oid)
		FROM pg_constraint k JOIN pg_class c ON c.oid = k.conrelid
		WHERE `+snapshotRelations+` AND k.contype IN ('p', 'u', 'c', 'f', 'x') ORDER BY c.relname, k.conname`, namespace)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var relation uint32
		var constraint schemadiff.Constraint
		if err := rows.Scan(&relation, &constraint.Name, &constraint.Quoted, &constraint.Kind, &constraint.Definition); err != nil {
			return err
		}
		position, ok := tables[relation]
		if !ok {
			continue
		}
		if err := budget.add(constraint.Name, constraint.Definition); err != nil {
			return err
		}
		result.Tables[position].Constraints = append(result.Tables[position].Constraints, constraint)
	}
	return rows.Err()
}

// Index milik PRIMARY KEY, UNIQUE, atau EXCLUDE sudah diwakili constraint-nya.
func snapshotIndexes(ctx context.Context, tx pgx.Tx, namespace uint32, tables map[uint32]int, result *schemadiff.Snapshot, budget *snapshotBudget) error {
	rows, err := tx.Query(ctx, `SELECT x.indrelid, i.relname, quote_ident(i.relname), x.indisunique, coalesce(substring(pg_get_indexdef(x.indexrelid) from ' USING .*$'), '')
		FROM pg_index x JOIN pg_class i ON i.oid = x.indexrelid JOIN pg_class c ON c.oid = x.indrelid
		WHERE `+snapshotRelations+` AND NOT EXISTS (
			SELECT 1 FROM pg_constraint k WHERE k.conindid = x.indexrelid AND k.conrelid = x.indrelid AND k.contype IN ('p', 'u', 'x'))
		ORDER BY c.relname, i.relname`, namespace)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var relation uint32
		var index schemadiff.Index
		if err := rows.Scan(&relation, &index.Name, &index.Quoted, &index.Unique, &index.Tail); err != nil {
			return err
		}
		position, ok := tables[relation]
		if !ok || index.Tail == "" {
			continue
		}
		if err := budget.add(index.Name, index.Tail); err != nil {
			return err
		}
		result.Tables[position].Indexes = append(result.Tables[position].Indexes, index)
	}
	return rows.Err()
}

func snapshotViews(ctx context.Context, tx pgx.Tx, namespace uint32, result *schemadiff.Snapshot, budget *snapshotBudget) error {
	rows, err := tx.Query(ctx, `SELECT c.relname, quote_ident(c.relname), pg_get_viewdef(c.oid)
		FROM pg_class c WHERE c.relnamespace = $1 AND c.relkind = 'v' ORDER BY c.relname`, namespace)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var view schemadiff.View
		if err := rows.Scan(&view.Name, &view.Quoted, &view.Definition); err != nil {
			return err
		}
		view.Definition = strings.TrimSuffix(strings.TrimSpace(view.Definition), ";")
		if err := budget.add(view.Name, view.Definition); err != nil {
			return err
		}
		result.Views = append(result.Views, view)
	}
	return rows.Err()
}
