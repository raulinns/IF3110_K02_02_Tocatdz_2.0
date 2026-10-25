package ui

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type ExplorerAdapter struct{ viewer explorer.Viewer }

var (
	_ DatabaseLister          = (*ExplorerAdapter)(nil)
	_ Explorer                = (*ExplorerAdapter)(nil)
	_ dbms.CapabilityResolver = (*ExplorerAdapter)(nil)
)

func NewExplorerAdapter(viewer explorer.Viewer) (*ExplorerAdapter, error) {
	if missingDependency(viewer) {
		return nil, security.ErrInvalidInput
	}
	return &ExplorerAdapter{viewer: viewer}, nil
}

func (a *ExplorerAdapter) Capabilities(ctx context.Context, id security.ID) (dbms.Capabilities, error) {
	resolver, ok := a.viewer.(dbms.CapabilityResolver)
	if !ok {
		return dbms.Features(dbms.PostgreSQL), nil
	}
	return resolver.Capabilities(ctx, id)
}

func (a *ExplorerAdapter) ListAccessible(ctx context.Context, principal security.Principal) ([]DatabaseSummary, error) {
	entries, err := a.viewer.ListAccessible(ctx, principal)
	if err != nil {
		return nil, err
	}
	result := make([]DatabaseSummary, 0, len(entries))
	for _, entry := range entries {
		result = append(result, DatabaseSummary{ID: entry.ID, Name: entry.Name, Access: entry.Access})
	}
	return result, nil
}

func explorerRequest(request DatabaseRequest) explorer.Request {
	return explorer.Request{Principal: request.Principal, DatabaseID: request.DatabaseID}
}

func (a *ExplorerAdapter) Schemas(ctx context.Context, request DatabaseRequest) (SchemaOverview, error) {
	schemas, err := a.viewer.Schemas(ctx, explorerRequest(request))
	if err != nil {
		return SchemaOverview{}, err
	}
	result := SchemaOverview{Schemas: make([]SchemaSummary, 0, len(schemas))}
	for _, schema := range schemas {
		item := SchemaSummary{Name: schema.Name, Tables: make([]TableSummary, 0, len(schema.Tables))}
		for _, table := range schema.Tables {
			item.Tables = append(item.Tables, TableSummary{Name: table.Name, Kind: string(table.Kind)})
		}
		result.Schemas = append(result.Schemas, item)
	}
	return result, nil
}

func (a *ExplorerAdapter) Table(ctx context.Context, request DatabaseRequest, schema, name string) (TableDetail, error) {
	table, err := a.viewer.Table(ctx, explorerRequest(request), schema, name)
	if err != nil {
		return TableDetail{}, err
	}
	result := TableDetail{Schema: table.Schema, Name: table.Name, Columns: make([]ColumnInfo, 0, len(table.Columns)), Constraints: make([]ConstraintInfo, 0, len(table.Constraints)), Indexes: make([]IndexInfo, 0, len(table.Indexes))}
	for _, column := range table.Columns {
		item := ColumnInfo{Name: column.Name, Type: column.DataType, Nullable: column.Nullable, HasDefault: column.Default != nil}
		if column.Default != nil {
			item.Default = *column.Default
		}
		result.Columns = append(result.Columns, item)
	}
	for _, constraint := range table.Constraints {
		result.Constraints = append(result.Constraints, ConstraintInfo{Name: constraint.Name, Kind: constraint.Type, Definition: constraint.Definition})
	}
	for _, index := range table.Indexes {
		result.Indexes = append(result.Indexes, IndexInfo{Name: index.Name, Definition: index.Definition, Unique: index.Unique, Primary: index.Primary})
	}
	return result, nil
}

func (a *ExplorerAdapter) Rows(ctx context.Context, request DatabaseRequest, query RowQuery) (RowPage, error) {
	page, err := a.viewer.Rows(ctx, explorerRequest(request), explorer.RowsQuery{Schema: query.Schema, Table: query.Table, Page: query.Page, Limit: query.Limit})
	if err != nil {
		return RowPage{}, err
	}
	result := RowPage{Columns: page.Columns, Rows: make([][]Cell, 0, len(page.Rows)), HasNext: page.HasNext, Order: page.Order}
	for _, row := range page.Rows {
		cells := make([]Cell, 0, len(row))
		for _, cell := range row {
			cells = append(cells, Cell{Value: cell.Text, Null: cell.Null, Truncated: cell.Truncated, Encoding: cell.Encoding})
		}
		result.Rows = append(result.Rows, cells)
	}
	return result, nil
}
