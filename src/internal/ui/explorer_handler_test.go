package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type fakeExplorer struct {
	calls    int
	request  DatabaseRequest
	schema   string
	table    string
	query    RowQuery
	overview SchemaOverview
	detail   TableDetail
	page     RowPage
	err      error
}

func (e *fakeExplorer) Schemas(_ context.Context, request DatabaseRequest) (SchemaOverview, error) {
	e.calls++
	e.request = request
	return e.overview, e.err
}

func (e *fakeExplorer) Table(_ context.Context, request DatabaseRequest, schema, table string) (TableDetail, error) {
	e.calls++
	e.request, e.schema, e.table = request, schema, table
	return e.detail, e.err
}

func (e *fakeExplorer) Rows(_ context.Context, request DatabaseRequest, query RowQuery) (RowPage, error) {
	e.calls++
	e.request, e.query = request, query
	return e.page, e.err
}

func testExplorerUI(t *testing.T, explorer *fakeExplorer, authorizer *fakeAuthorizer) *http.ServeMux {
	t.Helper()
	renderer, _ := NewRenderer()
	access, _ := NewDatabaseGuard(authorizer)
	ui, err := NewExplorerUI(explorer, &fakeGuard{}, access, renderer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ui.RegisterRoutes(mux)
	return mux
}

func serve(mux *http.ServeMux, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

var (
	schemaPath = "/databases/" + string(testDatabaseA) + "/schema"
	tablePath  = "/databases/" + string(testDatabaseA) + "/tables/public/users"
	rowsPath   = tablePath + "/rows"
)

func TestNewExplorerUIRejectsMissingDependencies(t *testing.T) {
	renderer, _ := NewRenderer()
	access, _ := NewDatabaseGuard(&fakeAuthorizer{})
	for _, build := range []func() (*ExplorerUI, error){
		func() (*ExplorerUI, error) { return NewExplorerUI(nil, &fakeGuard{}, access, renderer) },
		func() (*ExplorerUI, error) { return NewExplorerUI(&fakeExplorer{}, nil, access, renderer) },
		func() (*ExplorerUI, error) { return NewExplorerUI(&fakeExplorer{}, &fakeGuard{}, nil, renderer) },
		func() (*ExplorerUI, error) { return NewExplorerUI(&fakeExplorer{}, &fakeGuard{}, access, nil) },
	} {
		if ui, err := build(); ui != nil || !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing dependency accepted")
		}
	}
}

func TestSchemaPageListsTablesWithEscapedLinks(t *testing.T) {
	explorer := &fakeExplorer{overview: SchemaOverview{DatabaseName: "inventaris", Schemas: []SchemaSummary{
		{Name: "public", Tables: []TableSummary{{Name: "users", Kind: "table"}, {Name: "a/b", Kind: "view"}}},
		{Name: "kosong"},
	}}}
	authorizer := &fakeAuthorizer{}
	w := serve(testExplorerUI(t, explorer, authorizer), protectedRequest("GET", schemaPath, nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || authorizer.action != security.ActionViewSchema || explorer.request.DatabaseID != testDatabaseA || explorer.request.Principal != testPrincipal {
		t.Fatal("schema page not authorized or not loaded")
	}
	for _, want := range []string{"inventaris", "public", "/tables/public/users", "/tables/public/a%2fb", "belum memiliki table"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Fatal("schema page missing: " + want)
		}
	}
}

func TestTablePageShowsCatalogDetail(t *testing.T) {
	explorer := &fakeExplorer{detail: TableDetail{
		Schema: "public", Name: "users",
		Columns:     []ColumnInfo{{Name: "id", Type: "uuid", Default: "gen_random_uuid()"}, {Name: "email", Type: "text", Nullable: true}},
		Constraints: []ConstraintInfo{{Name: "users_pkey", Kind: "PRIMARY KEY", Definition: "PRIMARY KEY (id)"}},
		Indexes:     []IndexInfo{{Name: "users_email_key", Definition: "CREATE UNIQUE INDEX users_email_key ON public.users USING btree (email)", Unique: true}},
	}}
	w := serve(testExplorerUI(t, explorer, &fakeAuthorizer{}), protectedRequest("GET", tablePath, nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || explorer.schema != "public" || explorer.table != "users" {
		t.Fatal("table page not loaded")
	}
	for _, want := range []string{"gen_random_uuid()", "users_pkey", "PRIMARY KEY (id)", "users_email_key", "/tables/public/users/rows"} {
		if !strings.Contains(body, want) {
			t.Fatal("table page missing: " + want)
		}
	}
}

func TestTablePageDecodesEscapedIdentifiers(t *testing.T) {
	explorer := &fakeExplorer{detail: TableDetail{Schema: "public", Name: "a/b"}}
	w := serve(testExplorerUI(t, explorer, &fakeAuthorizer{}), protectedRequest("GET", "/databases/"+string(testDatabaseA)+"/tables/public/a%2Fb", nil))
	if w.Code != http.StatusOK || explorer.table != "a/b" {
		t.Fatal("escaped table name not passed intact")
	}
}

func TestTablePageRejectsMismatchedDetail(t *testing.T) {
	explorer := &fakeExplorer{detail: TableDetail{Schema: "public", Name: "secrets"}}
	w := serve(testExplorerUI(t, explorer, &fakeAuthorizer{}), protectedRequest("GET", tablePath, nil))
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secrets") {
		t.Fatal("detail for another table rendered")
	}
}

func TestRowsPagePaginatesAndShowsNull(t *testing.T) {
	explorer := &fakeExplorer{page: RowPage{
		Columns: []string{"id", "email"},
		Rows:    [][]Cell{{{Value: "1"}, {Null: true}}, {{Value: "2"}, {Value: "<b>x</b>"}}},
		HasNext: true,
	}}
	authorizer := &fakeAuthorizer{}
	w := serve(testExplorerUI(t, explorer, authorizer), protectedRequest("GET", rowsPath+"?page=2&limit=2", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || authorizer.action != security.ActionViewData || explorer.query != (RowQuery{Schema: "public", Table: "users", Page: 2, Limit: 2}) {
		t.Fatal("rows not requested with validated query")
	}
	if !strings.Contains(body, `class="null"`) || strings.Contains(body, "<b>x</b>") || !strings.Contains(body, "&lt;b&gt;x&lt;/b&gt;") {
		t.Fatal("null or untrusted cell rendered incorrectly")
	}
	if !strings.Contains(body, "rows?limit=2&amp;page=1") || !strings.Contains(body, "rows?limit=2&amp;page=3") {
		t.Fatal("pagination links missing")
	}
	w = serve(testExplorerUI(t, explorer, authorizer), protectedRequest("GET", rowsPath, nil))
	if explorer.query.Page != 1 || explorer.query.Limit != defaultRowLimit || strings.Contains(w.Body.String(), "page=0") {
		t.Fatal("default pagination wrong")
	}
}

func TestRowsPageRejectsInvalidQueryBeforeLoading(t *testing.T) {
	for _, query := range []string{"?limit=0", "?limit=201", "?page=0", "?page=abc", "?page=1&page=2", "?sort=email", "?limit=%zz"} {
		explorer := &fakeExplorer{}
		w := serve(testExplorerUI(t, explorer, &fakeAuthorizer{}), protectedRequest("GET", rowsPath+query, nil))
		if w.Code != http.StatusBadRequest || explorer.calls != 0 {
			t.Fatal("invalid row query reached explorer: " + query)
		}
	}
}

func TestRowsPageFailsClosedOnMalformedPage(t *testing.T) {
	for _, page := range []RowPage{
		{Columns: []string{"id"}, Rows: [][]Cell{{{Value: "1"}}, {{Value: "2"}}, {{Value: "3"}}}},
		{Columns: []string{"id", "email"}, Rows: [][]Cell{{{Value: "1"}}}},
	} {
		w := serve(testExplorerUI(t, &fakeExplorer{page: page}, &fakeAuthorizer{}), protectedRequest("GET", rowsPath+"?limit=2", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatal("malformed row page rendered")
		}
	}
}

func TestExplorerDeniesWithoutLoading(t *testing.T) {
	for _, target := range []string{schemaPath, tablePath, rowsPath} {
		explorer := &fakeExplorer{}
		w := serve(testExplorerUI(t, explorer, &fakeAuthorizer{databaseErr: security.ErrForbidden}), protectedRequest("GET", target, nil))
		if w.Code != http.StatusForbidden || explorer.calls != 0 || !strings.Contains(w.Body.String(), "Akses ditolak") {
			t.Fatal("denied explorer request loaded data: " + target)
		}
	}
	explorer := &fakeExplorer{}
	w := serve(testExplorerUI(t, explorer, &fakeAuthorizer{}), protectedRequest("GET", "/databases/"+string(testDatabaseA)+"/tables/public/bad%0Aname", nil))
	if w.Code != http.StatusBadRequest || explorer.calls != 0 {
		t.Fatal("invalid identifier reached explorer")
	}
}

func TestExplorerFailuresAreSanitized(t *testing.T) {
	for _, scenario := range []struct {
		err    error
		status int
	}{
		{security.ErrUnavailable, http.StatusServiceUnavailable},
		{security.ErrNotFound, http.StatusNotFound},
		{errors.New("pq: password authentication failed for canary-secret"), http.StatusInternalServerError},
	} {
		w := serve(testExplorerUI(t, &fakeExplorer{err: scenario.err}, &fakeAuthorizer{}), protectedRequest("GET", tablePath, nil))
		if w.Code != scenario.status || strings.Contains(w.Body.String(), "canary-secret") {
			t.Fatal("explorer failure leaked or mapped wrong")
		}
	}
}
