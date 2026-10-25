package schemadiff

import (
	"strconv"
	"strings"
)

type phase int

const (
	dropViews phase = iota
	dropForeignKeys
	dropConstraints
	dropIndexes
	alterColumns
	dropTables
	createTables
	addConstraints
	addForeignKeys
	createIndexes
	createViews
	phaseCount
)

type plan struct {
	current, desired Snapshot
	changes          []Change
	warnings         []string
	statements       [phaseCount][]string
}

// Compare menghasilkan perubahan dan SQL yang mengubah schema current menjadi seperti desired.
// Rename tidak dideteksi: objek dengan nama berbeda dianggap dihapus lalu dibuat baru.
func Compare(current, desired Snapshot) ([]Change, []string, string) {
	p := &plan{current: current, desired: remap(desired, current)}
	tables := map[string]Table{}
	for _, table := range current.Tables {
		tables[table.Name] = table
	}
	wanted := map[string]bool{}
	for _, table := range p.desired.Tables {
		wanted[table.Name] = true
		if old, ok := tables[table.Name]; ok {
			p.alterTable(old, table)
			continue
		}
		p.createTable(table)
	}
	for _, table := range current.Tables {
		if !wanted[table.Name] {
			p.dropTable(table)
		}
	}
	p.compareViews()
	return p.changes, p.warnings, p.sql()
}

// Definisi dari schema lain di-quote dengan nama schema-nya; acuan ke schema itu diarahkan ke schema current.
func remap(desired, current Snapshot) Snapshot {
	if desired.Quoted == current.Quoted || desired.Quoted == "" {
		return desired
	}
	from, to := desired.Quoted+".", current.Quoted+"."
	swap := func(text string) string { return strings.ReplaceAll(text, from, to) }
	result := Snapshot{Schema: current.Schema, Quoted: current.Quoted, Tables: make([]Table, 0, len(desired.Tables)), Views: make([]View, 0, len(desired.Views))}
	for _, table := range desired.Tables {
		copied := Table{Name: table.Name, Quoted: table.Quoted}
		for _, column := range table.Columns {
			if column.Default != nil {
				value := swap(*column.Default)
				column.Default = &value
			}
			copied.Columns = append(copied.Columns, column)
		}
		for _, constraint := range table.Constraints {
			constraint.Definition = swap(constraint.Definition)
			copied.Constraints = append(copied.Constraints, constraint)
		}
		for _, index := range table.Indexes {
			index.Tail = swap(index.Tail)
			copied.Indexes = append(copied.Indexes, index)
		}
		result.Tables = append(result.Tables, copied)
	}
	for _, view := range desired.Views {
		view.Definition = swap(view.Definition)
		result.Views = append(result.Views, view)
	}
	return result
}

func (p *plan) add(at phase, statement string) {
	p.statements[at] = append(p.statements[at], statement+";")
}

func (p *plan) record(change Change) {
	p.changes = append(p.changes, change)
}

func (p *plan) qualified(name string) string { return p.current.Quoted + "." + name }

func (p *plan) createTable(table Table) {
	lines := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		lines = append(lines, "    "+columnDefinition(column))
	}
	statement := "CREATE TABLE " + p.qualified(table.Quoted) + " ()"
	if len(lines) > 0 {
		statement = "CREATE TABLE " + p.qualified(table.Quoted) + " (\n" + strings.Join(lines, ",\n") + "\n)"
	}
	p.add(createTables, statement)
	p.record(Change{Action: ActionAdd, Object: "Tabel", Name: table.Name, After: columnCount(len(table.Columns))})
	for _, constraint := range table.Constraints {
		p.addConstraint(table, constraint, false)
	}
	for _, index := range table.Indexes {
		p.createIndex(table, index, false)
	}
}

// Foreign key tabel yang dihapus dilepas lebih dulu supaya urutan DROP TABLE antar tabel yang saling mengacu tidak berpengaruh.
func (p *plan) dropTable(table Table) {
	for _, constraint := range table.Constraints {
		if constraint.Kind == "f" {
			p.dropConstraint(table, constraint, true)
		}
	}
	p.add(dropTables, "DROP TABLE "+p.qualified(table.Quoted))
	p.record(Change{Action: ActionDrop, Object: "Tabel", Name: table.Name, Before: columnCount(len(table.Columns)), Destructive: true})
}

func columnCount(count int) string {
	if count == 1 {
		return "1 kolom"
	}
	return strconv.Itoa(count) + " kolom"
}

func (p *plan) alterTable(old, table Table) {
	target := p.qualified(table.Quoted)
	columns := map[string]Column{}
	for _, column := range old.Columns {
		columns[column.Name] = column
	}
	wanted := map[string]bool{}
	for _, column := range table.Columns {
		wanted[column.Name] = true
		before, ok := columns[column.Name]
		if !ok {
			p.add(alterColumns, "ALTER TABLE "+target+" ADD COLUMN "+columnDefinition(column))
			p.record(Change{Action: ActionAdd, Object: "Kolom", Name: table.Name + "." + column.Name, After: describe(column)})
			if column.NotNull && column.Default == nil && column.Identity == "" && !column.Serial && column.Generated == "" {
				p.warn("Kolom " + table.Name + "." + column.Name + " NOT NULL tanpa default akan gagal ditambahkan jika tabel sudah berisi data.")
			}
			continue
		}
		p.alterColumn(table, target, before, column)
	}
	for _, column := range old.Columns {
		if !wanted[column.Name] {
			p.add(alterColumns, "ALTER TABLE "+target+" DROP COLUMN "+column.Quoted)
			p.record(Change{Action: ActionDrop, Object: "Kolom", Name: table.Name + "." + column.Name, Before: describe(column), Destructive: true})
		}
	}
	p.compareConstraints(old, table)
	p.compareIndexes(old, table)
}

func (p *plan) alterColumn(table Table, target string, before, after Column) {
	name := table.Name + "." + after.Name
	column := "ALTER TABLE " + target + " ALTER COLUMN " + after.Quoted
	changed := false
	if before.Type != after.Type {
		p.add(alterColumns, column+" TYPE "+after.Type)
		p.warn("Tipe " + name + " berubah dari " + before.Type + " ke " + after.Type + "; tambahkan USING bila data lama tidak bisa dikonversi otomatis.")
		changed = true
	}
	special := before.Identity != after.Identity || before.Serial != after.Serial || before.Generated != after.Generated
	if special {
		p.warn("Identity, serial, atau generated column pada " + name + " berbeda; perubahan ini tidak dibuat otomatis dan perlu ditulis manual.")
		changed = true
	} else if after.Identity == "" && !after.Serial && !sameText(before.Default, after.Default) {
		switch {
		case after.Generated != "":
			p.warn("Ekspresi generated column " + name + " berbeda; perubahan ini perlu ditulis manual.")
		case after.Default == nil:
			p.add(alterColumns, column+" DROP DEFAULT")
		default:
			p.add(alterColumns, column+" SET DEFAULT "+*after.Default)
		}
		changed = true
	}
	if before.NotNull != after.NotNull {
		if after.NotNull {
			p.add(alterColumns, column+" SET NOT NULL")
			p.warn("SET NOT NULL pada " + name + " akan gagal jika masih ada baris bernilai NULL.")
		} else {
			p.add(alterColumns, column+" DROP NOT NULL")
		}
		changed = true
	}
	if changed {
		p.record(Change{Action: ActionAlter, Object: "Kolom", Name: name, Before: describe(before), After: describe(after)})
	}
}

func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (p *plan) compareConstraints(old, table Table) {
	existing := map[string]Constraint{}
	for _, constraint := range old.Constraints {
		existing[constraint.Name] = constraint
	}
	wanted := map[string]bool{}
	for _, constraint := range table.Constraints {
		wanted[constraint.Name] = true
		before, ok := existing[constraint.Name]
		switch {
		case !ok:
			p.addConstraint(table, constraint, false)
		case before.Kind != constraint.Kind || before.Definition != constraint.Definition:
			p.dropConstraint(table, before, true)
			p.addConstraint(table, constraint, true)
			p.record(Change{Action: ActionAlter, Object: "Constraint", Name: table.Name + "." + constraint.Name, Before: before.Definition, After: constraint.Definition})
		}
	}
	for _, constraint := range old.Constraints {
		if !wanted[constraint.Name] {
			p.dropConstraint(table, constraint, false)
		}
	}
}

func (p *plan) addConstraint(table Table, constraint Constraint, silent bool) {
	at := addConstraints
	if constraint.Kind == "f" {
		at = addForeignKeys
	}
	p.add(at, "ALTER TABLE "+p.qualified(table.Quoted)+" ADD CONSTRAINT "+constraint.Quoted+" "+constraint.Definition)
	if !silent {
		p.record(Change{Action: ActionAdd, Object: "Constraint", Name: table.Name + "." + constraint.Name, After: constraint.Definition})
	}
}

func (p *plan) dropConstraint(table Table, constraint Constraint, silent bool) {
	at := dropConstraints
	if constraint.Kind == "f" {
		at = dropForeignKeys
	}
	p.add(at, "ALTER TABLE "+p.qualified(table.Quoted)+" DROP CONSTRAINT "+constraint.Quoted)
	if !silent {
		p.record(Change{Action: ActionDrop, Object: "Constraint", Name: table.Name + "." + constraint.Name, Before: constraint.Definition})
	}
}

func (p *plan) compareIndexes(old, table Table) {
	existing := map[string]Index{}
	for _, index := range old.Indexes {
		existing[index.Name] = index
	}
	wanted := map[string]bool{}
	for _, index := range table.Indexes {
		wanted[index.Name] = true
		before, ok := existing[index.Name]
		switch {
		case !ok:
			p.createIndex(table, index, false)
		case before.Unique != index.Unique || before.Tail != index.Tail:
			p.dropIndex(before)
			p.createIndex(table, index, true)
			p.record(Change{Action: ActionAlter, Object: "Index", Name: table.Name + "." + index.Name, Before: indexLabel(before), After: indexLabel(index)})
		}
	}
	for _, index := range old.Indexes {
		if !wanted[index.Name] {
			p.dropIndex(index)
			p.record(Change{Action: ActionDrop, Object: "Index", Name: table.Name + "." + index.Name, Before: indexLabel(index)})
		}
	}
}

func indexLabel(index Index) string {
	if index.Unique {
		return "UNIQUE" + index.Tail
	}
	return strings.TrimSpace(index.Tail)
}

func (p *plan) createIndex(table Table, index Index, silent bool) {
	kind := "CREATE INDEX "
	if index.Unique {
		kind = "CREATE UNIQUE INDEX "
	}
	p.add(createIndexes, kind+index.Quoted+" ON "+p.qualified(table.Quoted)+index.Tail)
	if !silent {
		p.record(Change{Action: ActionAdd, Object: "Index", Name: table.Name + "." + index.Name, After: indexLabel(index)})
	}
}

func (p *plan) dropIndex(index Index) {
	p.add(dropIndexes, "DROP INDEX "+p.qualified(index.Quoted))
}

func (p *plan) compareViews() {
	existing := map[string]View{}
	for _, view := range p.current.Views {
		existing[view.Name] = view
	}
	wanted := map[string]bool{}
	for _, view := range p.desired.Views {
		wanted[view.Name] = true
		before, ok := existing[view.Name]
		if ok && before.Definition == view.Definition {
			continue
		}
		if ok {
			p.add(dropViews, "DROP VIEW "+p.qualified(view.Quoted))
			p.record(Change{Action: ActionAlter, Object: "View", Name: view.Name, Before: oneLine(before.Definition), After: oneLine(view.Definition)})
		} else {
			p.record(Change{Action: ActionAdd, Object: "View", Name: view.Name, After: oneLine(view.Definition)})
		}
		p.add(createViews, "CREATE VIEW "+p.qualified(view.Quoted)+" AS\n"+view.Definition)
	}
	for _, view := range p.current.Views {
		if !wanted[view.Name] {
			p.add(dropViews, "DROP VIEW "+p.qualified(view.Quoted))
			p.record(Change{Action: ActionDrop, Object: "View", Name: view.Name, Before: oneLine(view.Definition)})
		}
	}
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func (p *plan) warn(message string) {
	p.warnings = append(p.warnings, message)
}

func columnDefinition(column Column) string {
	kind := column.Type
	if column.Serial {
		switch kind {
		case "integer":
			kind = "serial"
		case "bigint":
			kind = "bigserial"
		case "smallint":
			kind = "smallserial"
		}
	}
	parts := []string{column.Quoted, kind}
	switch {
	case column.Generated != "" && column.Default != nil:
		storage := "STORED"
		if column.Generated == "v" {
			storage = "VIRTUAL"
		}
		parts = append(parts, "GENERATED ALWAYS AS ("+*column.Default+") "+storage)
	case column.Identity == "a":
		parts = append(parts, "GENERATED ALWAYS AS IDENTITY")
	case column.Identity == "d":
		parts = append(parts, "GENERATED BY DEFAULT AS IDENTITY")
	case column.Default != nil && !column.Serial:
		parts = append(parts, "DEFAULT "+*column.Default)
	}
	if column.NotNull {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " ")
}

func describe(column Column) string {
	text := columnDefinition(column)
	return strings.TrimSpace(strings.TrimPrefix(text, column.Quoted))
}

func (p *plan) sql() string {
	var builder strings.Builder
	for _, statements := range p.statements {
		if len(statements) == 0 {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		for _, statement := range statements {
			builder.WriteString(statement)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}
