package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type EngineResolver interface {
	Engine(context.Context, security.ID) (dbms.Engine, error)
}

type ExplorerConfig struct {
	Viewer  explorer.Viewer
	Diff    schemadiff.Application
	Engines EngineResolver
}

const explorerDatabasePath = "/databases/{database_id}"

// ExplorerRoutes accepts the authorized explorer and schema diff services.
// Append the returned routes to Config.Routes when assembling the API.
func ExplorerRoutes(cfg ExplorerConfig) ([]Route, error) {
	for _, dep := range []any{cfg.Viewer, cfg.Diff, cfg.Engines} {
		if missingAuthDependency(dep) {
			return nil, fmt.Errorf("explorer dependencies are required: %w", security.ErrInvalidInput)
		}
	}
	table := explorerDatabasePath + "/tables/{schema}/{table}"
	return []Route{
		{Method: http.MethodGet, Path: "/databases", QueryParameters: []string{"page", "limit"}, Handle: cfg.databases},
		{Method: http.MethodGet, Path: explorerDatabasePath + "/schema", Handle: cfg.schemas},
		{Method: http.MethodGet, Path: table, Handle: cfg.table},
		{Method: http.MethodGet, Path: table + "/rows", QueryParameters: []string{"page", "limit"}, Handle: cfg.rows},
		{Method: http.MethodGet, Path: explorerDatabasePath + "/schema-diff", QueryParameters: []string{"desired_database_id", "current_schema", "desired_schema"}, Handle: cfg.diff},
	}, nil
}

type explorerPage struct {
	Page    int  `json:"page"`
	Limit   int  `json:"limit"`
	HasNext bool `json:"has_next"`
}

type explorerCapabilities struct {
	SchemaDiff bool `json:"schema_diff"`
	QueryPlans bool `json:"query_plans"`
	Workflow   bool `json:"workflow"`
	Batch      bool `json:"batch"`
}

type explorerDatabase struct {
	ID           security.ID             `json:"id"`
	Name         string                  `json:"name"`
	Access       security.DatabaseAccess `json:"access"`
	Engine       dbms.Engine             `json:"engine"`
	Capabilities explorerCapabilities    `json:"capabilities"`
}

func (cfg ExplorerConfig) databases(w http.ResponseWriter, r *http.Request) error {
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return security.ErrUnauthenticated
	}
	page, limit, offset, err := explorer.ParsePagination(r.URL.Query())
	if err != nil {
		return security.ErrMalformedRequest
	}
	ctx, cancel := context.WithTimeout(r.Context(), explorer.ReadTimeout)
	defer cancel()
	entries, err := cfg.Viewer.ListAccessible(ctx, principal)
	if err != nil {
		return err
	}
	// ListAccessible already orders authorized entries by name then ID.
	start := min(int(offset), len(entries))
	end := min(start+limit, len(entries))
	items := make([]explorerDatabase, 0, end-start)
	for _, entry := range entries[start:end] {
		engine, err := cfg.Engines.Engine(ctx, entry.ID)
		if err != nil {
			return err
		}
		if engine != dbms.PostgreSQL && engine != dbms.MySQL {
			return security.ErrUnavailable
		}
		features := dbms.Features(engine)
		items = append(items, explorerDatabase{
			ID: entry.ID, Name: entry.Name, Access: entry.Access, Engine: engine,
			Capabilities: explorerCapabilities{
				SchemaDiff: features.SchemaDiff, QueryPlans: features.QueryPlans,
				Workflow: engine == dbms.PostgreSQL, Batch: engine == dbms.PostgreSQL,
			},
		})
	}
	return WriteJSON(w, http.StatusOK, struct {
		explorerPage
		Items []explorerDatabase `json:"items"`
	}{explorerPage{page, limit, end < len(entries)}, items})
}

func explorerRequest(r *http.Request) (explorer.Request, error) {
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return explorer.Request{}, security.ErrUnauthenticated
	}
	id, err := PathID(r, "database_id")
	return explorer.Request{Principal: principal, DatabaseID: id}, err
}

func (cfg ExplorerConfig) schemas(w http.ResponseWriter, r *http.Request) error {
	request, err := explorerRequest(r)
	if err != nil {
		return err
	}
	schemas, err := cfg.Viewer.Schemas(r.Context(), request)
	if err != nil {
		return err
	}
	schemas = nonNil(slices.Clone(schemas))
	for i := range schemas {
		schemas[i].Tables = nonNil(schemas[i].Tables)
	}
	return WriteJSON(w, http.StatusOK, struct {
		Schemas []explorer.Schema `json:"schemas"`
	}{schemas})
}

func (cfg ExplorerConfig) table(w http.ResponseWriter, r *http.Request) error {
	request, err := explorerRequest(r)
	if err != nil {
		return err
	}
	query, err := explorer.ParseRowsQuery(r.PathValue("schema"), r.PathValue("table"), nil)
	if err != nil {
		return security.ErrMalformedRequest
	}
	table, err := cfg.Viewer.Table(r.Context(), request, query.Schema, query.Table)
	if err != nil {
		return err
	}
	table.Columns = nonNil(table.Columns)
	table.Constraints = nonNil(table.Constraints)
	table.Indexes = nonNil(table.Indexes)
	return WriteJSON(w, http.StatusOK, table)
}

func (cfg ExplorerConfig) rows(w http.ResponseWriter, r *http.Request) error {
	request, err := explorerRequest(r)
	if err != nil {
		return err
	}
	query, err := explorer.ParseRowsQuery(r.PathValue("schema"), r.PathValue("table"), r.URL.Query())
	if err != nil {
		return security.ErrMalformedRequest
	}
	result, err := cfg.Viewer.Rows(r.Context(), request, query)
	if err != nil {
		return err
	}
	items := make([][]explorer.Cell, 0, len(result.Rows))
	for _, row := range result.Rows {
		cells := nonNil(slices.Clone(row))
		for i := range cells {
			if cells[i].Encoding == "" {
				cells[i].Encoding = explorer.EncodingText
			}
		}
		items = append(items, cells)
	}
	return writeJSON(w, http.StatusOK, struct {
		explorerPage
		Columns []string          `json:"columns"`
		Items   [][]explorer.Cell `json:"items"`
		Order   explorer.RowOrder `json:"order"`
	}{explorerPage{result.Page, result.Limit, result.HasNext}, nonNil(result.Columns), items, result.Order}, explorer.MaxPayloadBytes)
}

type schemaChange struct {
	Action      schemadiff.ChangeAction `json:"action"`
	Object      string                  `json:"object"`
	Name        string                  `json:"name"`
	Before      string                  `json:"before"`
	After       string                  `json:"after"`
	Destructive bool                    `json:"destructive"`
}

func (cfg ExplorerConfig) diff(w http.ResponseWriter, r *http.Request) error {
	request, err := explorerRequest(r)
	if err != nil {
		return err
	}
	query := r.URL.Query()
	desiredID := security.ID(query.Get("desired_database_id"))
	if !security.ValidID(desiredID) {
		return security.ErrMalformedRequest
	}
	current, desired := diffSchema(query, "current_schema"), diffSchema(query, "desired_schema")
	if !schemadiff.ValidSchemaName(current) || !schemadiff.ValidSchemaName(desired) {
		return security.ErrMalformedRequest
	}
	// Compare checks both grants before opening either catalog and rechecks during comparison.
	result, err := cfg.Diff.Compare(r.Context(), schemadiff.Request{
		Principal: request.Principal, CurrentID: request.DatabaseID, DesiredID: desiredID,
		CurrentSchema: current, DesiredSchema: desired,
	})
	if err != nil {
		return err
	}
	changes := make([]schemaChange, 0, len(result.Changes))
	for _, c := range result.Changes {
		changes = append(changes, schemaChange{
			Action: c.Action, Object: c.Object, Name: c.Name,
			Before: c.Before, After: c.After, Destructive: c.Destructive,
		})
	}
	return WriteJSON(w, http.StatusOK, struct {
		CurrentID        security.ID    `json:"current_database_id"`
		DesiredID        security.ID    `json:"desired_database_id"`
		CurrentSchema    string         `json:"current_schema"`
		DesiredSchema    string         `json:"desired_schema"`
		Changes          []schemaChange `json:"changes"`
		Warnings         []string       `json:"warnings"`
		SQL              string         `json:"generated_up_sql"`
		Validation       string         `json:"validation"`
		DestructiveCount int            `json:"destructive_count"`
	}{
		CurrentID: request.DatabaseID, DesiredID: desiredID, CurrentSchema: current, DesiredSchema: desired,
		Changes: changes, Warnings: nonNil(result.Warnings), SQL: result.SQL.Reveal(),
		Validation: result.Validation, DestructiveCount: result.Destructive,
	})
}

func diffSchema(query url.Values, key string) string {
	if !query.Has(key) {
		return "public"
	}
	return query.Get(key)
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
