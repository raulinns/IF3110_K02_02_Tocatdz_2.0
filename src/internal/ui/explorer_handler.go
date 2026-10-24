package ui

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	defaultRowLimit = 50
	maxRowLimit     = 200
	maxRowPage      = 1_000_000
)

type SchemaOverview struct {
	DatabaseName string
	Schemas      []SchemaSummary
}

type SchemaSummary struct {
	Name   string
	Tables []TableSummary
}

type TableSummary struct {
	Name string
	Kind string
}

type ColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	Default  string
}

type ConstraintInfo struct {
	Name       string
	Kind       string
	Definition string
}

type IndexInfo struct {
	Name       string
	Definition string
	Unique     bool
}

type TableDetail struct {
	Schema      string
	Name        string
	Columns     []ColumnInfo
	Constraints []ConstraintInfo
	Indexes     []IndexInfo
}

type RowQuery struct {
	Schema string
	Table  string
	Page   int
	Limit  int
}

type Cell struct {
	Value string
	Null  bool
}

type RowPage struct {
	Columns []string
	Rows    [][]Cell
	HasNext bool
}

// Implementasi tetap wajib mengotorisasi request sendiri sebelum membuka koneksi ke database target
type Explorer interface {
	Schemas(ctx context.Context, request DatabaseRequest) (SchemaOverview, error)
	Table(ctx context.Context, request DatabaseRequest, schema, table string) (TableDetail, error)
	Rows(ctx context.Context, request DatabaseRequest, query RowQuery) (RowPage, error)
}

type SchemaView struct {
	DatabaseID security.ID
	Overview   SchemaOverview
}

func (v SchemaView) Tabs() DatabaseTabs { return databaseTabs(v.DatabaseID, "schema") }

func (v SchemaView) TableHref(schema, table string) string {
	return databasePath(v.DatabaseID, "tables", schema, table)
}

type TableView struct {
	DatabaseID security.ID
	Table      TableDetail
}

func (v TableView) Tabs() DatabaseTabs { return databaseTabs(v.DatabaseID, "schema") }

func (v TableView) RowsHref() string {
	return databasePath(v.DatabaseID, "tables", v.Table.Schema, v.Table.Name, "rows")
}

type RowsView struct {
	DatabaseID security.ID
	Query      RowQuery
	Page       RowPage
}

func (v RowsView) Tabs() DatabaseTabs { return databaseTabs(v.DatabaseID, "schema") }

func (v RowsView) TableHref() string {
	return databasePath(v.DatabaseID, "tables", v.Query.Schema, v.Query.Table)
}

func (v RowsView) pageHref(page int) string {
	query := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(v.Query.Limit)}}
	return databasePath(v.DatabaseID, "tables", v.Query.Schema, v.Query.Table, "rows") + "?" + query.Encode()
}

func (v RowsView) PrevHref() string {
	if v.Query.Page <= 1 {
		return ""
	}
	return v.pageHref(v.Query.Page - 1)
}

func (v RowsView) NextHref() string {
	if !v.Page.HasNext {
		return ""
	}
	return v.pageHref(v.Query.Page + 1)
}

type ExplorerUI struct {
	explorer Explorer
	guard    security.Middleware
	access   *DatabaseGuard
	renderer *Renderer
}

var _ security.RouteRegistrar = (*ExplorerUI)(nil)

func NewExplorerUI(explorer Explorer, guard security.Middleware, access *DatabaseGuard, renderer *Renderer) (*ExplorerUI, error) {
	for _, dependency := range []any{explorer, guard, access, renderer} {
		if missingDependency(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &ExplorerUI{explorer: explorer, guard: guard, access: access, renderer: renderer}, nil
}

func (ui *ExplorerUI) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /databases/{databaseID}/schema", ui.guard.Protected(http.HandlerFunc(ui.schema)))
	mux.Handle("GET /databases/{databaseID}/tables/{schema}/{table}", ui.guard.Protected(http.HandlerFunc(ui.table)))
	mux.Handle("GET /databases/{databaseID}/tables/{schema}/{table}/rows", ui.guard.Protected(http.HandlerFunc(ui.rows)))
}

func (ui *ExplorerUI) schema(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionViewSchema)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	overview, err := ui.explorer.Schemas(r.Context(), request)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	_ = ui.renderer.Render(w, r, http.StatusOK, "explorer/schema", Page{Title: "Schema", Content: SchemaView{DatabaseID: request.DatabaseID, Overview: overview}})
}

func tableParams(r *http.Request) (string, string, error) {
	schema, err := pathIdentifier(r, "schema")
	if err != nil {
		return "", "", err
	}
	table, err := pathIdentifier(r, "table")
	if err != nil {
		return "", "", err
	}
	return schema, table, nil
}

func (ui *ExplorerUI) table(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionViewSchema)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	schema, table, err := tableParams(r)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	detail, err := ui.explorer.Table(r.Context(), request, schema, table)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	if detail.Schema != schema || detail.Name != table {
		_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
		return
	}
	_ = ui.renderer.Render(w, r, http.StatusOK, "explorer/table", Page{Title: schema + "." + table, Content: TableView{DatabaseID: request.DatabaseID, Table: detail}})
}

func rowQuery(r *http.Request, schema, table string) (RowQuery, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return RowQuery{}, security.ErrInvalidInput
	}
	for key, value := range values {
		if key != "page" && key != "limit" || len(value) != 1 {
			return RowQuery{}, security.ErrInvalidInput
		}
	}
	page, err := boundedQueryInt(values, "page", 1, 1, maxRowPage)
	if err != nil {
		return RowQuery{}, err
	}
	limit, err := boundedQueryInt(values, "limit", defaultRowLimit, 1, maxRowLimit)
	if err != nil {
		return RowQuery{}, err
	}
	return RowQuery{Schema: schema, Table: table, Page: page, Limit: limit}, nil
}

func validRowPage(page RowPage, limit int) bool {
	if len(page.Rows) > limit {
		return false
	}
	for _, row := range page.Rows {
		if len(row) != len(page.Columns) {
			return false
		}
	}
	return true
}

func (ui *ExplorerUI) rows(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionViewData)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	schema, table, err := tableParams(r)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	query, err := rowQuery(r, schema, table)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	page, err := ui.explorer.Rows(r.Context(), request, query)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	if !validRowPage(page, query.Limit) {
		_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
		return
	}
	_ = ui.renderer.Render(w, r, http.StatusOK, "explorer/rows", Page{Title: "Data " + schema + "." + table, Content: RowsView{DatabaseID: request.DatabaseID, Query: query, Page: page}})
}
