package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	explorerDesired security.ID = "10000000-0000-4000-8000-000000000002"
	explorerDenied  security.ID = "10000000-0000-4000-8000-000000000003"
	explorerMySQL   security.ID = "10000000-0000-4000-8000-000000000004"
)

type catalogFixture struct {
	reads       int
	engineReads int
	snapshots   int
	options     explorer.RowReadOptions
}

func (*catalogFixture) ListAccessible(context.Context, security.Principal) ([]explorer.DatabaseSummary, error) {
	return []explorer.DatabaseSummary{
		{ID: explorerMySQL, Name: "Z test", Access: security.AccessViewOnly},
		{ID: explorerDenied, Name: "A denied", Access: security.AccessViewOnly},
		{ID: testDatabase, Name: "B test", Access: security.AccessViewOnly},
	}, nil
}

func (c *catalogFixture) Engine(_ context.Context, id security.ID) (dbms.Engine, error) {
	c.engineReads++
	switch id {
	case testDatabase:
		return dbms.PostgreSQL, nil
	case explorerMySQL:
		return dbms.MySQL, nil
	default:
		return "", security.ErrNotFound
	}
}

func (c *catalogFixture) Schemas(context.Context, security.ID) ([]explorer.Schema, error) {
	c.reads++
	return []explorer.Schema{{Name: "public"}}, nil
}

func (c *catalogFixture) Table(_ context.Context, _ security.ID, schema, name string) (explorer.Table, error) {
	c.reads++
	return explorer.Table{Schema: schema, Name: name, Kind: explorer.KindTable}, nil
}

func (c *catalogFixture) Rows(_ context.Context, _ security.ID, _, _ string, options explorer.RowReadOptions) (explorer.RowBatch, error) {
	c.reads++
	c.options = options
	return explorer.RowBatch{
		Columns: []string{"a", "b"}, Order: explorer.OrderPrimaryKey,
		Rows: [][]explorer.Cell{
			{{Null: true}, explorer.PreviewBinary([]byte{0xff})},
			{{Text: "next"}, {Null: true}},
		},
	}, nil
}

func (c *catalogFixture) Snapshot(_ context.Context, id security.ID, schema string) (schemadiff.Snapshot, error) {
	c.snapshots++
	snapshot := schemadiff.Snapshot{Schema: schema, Quoted: schema}
	if id == explorerDesired {
		snapshot.Tables = []schemadiff.Table{{Name: "widgets", Quoted: "widgets", Columns: []schemadiff.Column{{Name: "id", Quoted: "id", Type: "integer"}}}}
	}
	return snapshot, nil
}

func TestExplorerRoutesIntegrateSessionAndDatabaseGrants(t *testing.T) {
	f := newFixture(t)
	f.store.additionalAccess = map[security.ID]security.DatabaseAccess{
		explorerDesired: security.AccessViewOnly,
		explorerMySQL:   security.AccessViewOnly,
	}
	session := fixtureLogin(t, f)
	catalog := &catalogFixture{}
	viewer, err := explorer.NewService(explorer.Config{Directory: catalog, Reader: catalog, Authorizer: f.authorizer})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := schemadiff.NewService(schemadiff.Config{Reader: catalog, Validator: migration.NewPostgreSQLValidator(), Authorizer: f.authorizer})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ExplorerRoutes(ExplorerConfig{Viewer: viewer, Diff: diff, Engines: catalog})
	if err != nil {
		t.Fatal(err)
	}
	a := buildAPI(t, f, routes...)
	get := func(path string, status int, fragments ...string) {
		t.Helper()
		w := dispatch(a, request(http.MethodGet, Prefix+path, "", session.CookieToken, security.Secret{}))
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET %s: status=%d cache-control=%q body=%s", path, w.Code, w.Header().Get("Cache-Control"), w.Body)
		}
		for _, fragment := range fragments {
			if !strings.Contains(w.Body.String(), fragment) {
				t.Errorf("GET %s: wanted %s in %s", path, fragment, w.Body)
			}
		}
	}
	current := "/databases/" + string(testDatabase)
	denied := "/databases/" + string(explorerDenied)
	w := dispatch(a, request(http.MethodGet, Prefix+current+"/schema", "", security.Secret{}, security.Secret{}))
	if w.Code != http.StatusUnauthorized || catalog.reads != 0 {
		t.Fatalf("anonymous schema request: status=%d reads=%d", w.Code, catalog.reads)
	}
	get("/databases?limit=1", 200, `"name":"B test"`, `"access":"view_only"`, `"engine":"postgresql"`, `"schema_diff":true`, `"has_next":true`)
	get("/databases?page=2&limit=1", 200, `"name":"Z test"`, `"engine":"mysql"`, `"schema_diff":false`, `"has_next":false`)
	get("/databases?page=3&limit=1", 200, `"items":[]`)
	if catalog.engineReads != 2 {
		t.Fatalf("expected metadata lookup for two authorized page entries, got %d", catalog.engineReads)
	}
	get(current+"/schema", 200, `"schemas":[{"name":"public","tables":[]}]`)
	table := "/tables/public/Items%20With%20Spaces"
	get(current+table, 200, `"name":"Items With Spaces"`, `"constraints":[]`, `"indexes":[]`)
	get(current+table+"/rows?page=2&limit=1", 200,
		`"items":[[{"text":"","null":true,"truncated":false,"encoding":"text"},{"text":"\\xff","null":false,"truncated":false,"encoding":"hex"}]]`,
		`"order":"primary_key"`, `"has_next":true`)
	if catalog.options.Offset != 1 || catalog.options.FetchLimit != 2 {
		t.Fatalf("unexpected read options: %+v", catalog.options)
	}
	get(denied+"/schema", 403, `"code":"forbidden"`)
	get(denied+table, 403, `"code":"forbidden"`)
	get(denied+table+"/rows", 403, `"code":"forbidden"`)
	get(current+table+"/rows?limit=1&limit=2", 400, `"code":"bad_request"`)
	get(current+table+"/rows?limit=201", 400, `"code":"bad_request"`)
	if catalog.reads != 3 {
		t.Fatalf("denied or malformed requests reached the reader: reads=%d", catalog.reads)
	}
	get(current+"/schema-diff?desired_database_id="+string(explorerDenied), 403, `"code":"forbidden"`)
	get(denied+"/schema-diff?desired_database_id="+string(explorerDesired), 403, `"code":"forbidden"`)
	get(current+"/schema-diff?desired_database_id="+string(explorerDesired)+"&current_schema=", 400, `"code":"bad_request"`)
	if catalog.snapshots != 0 {
		t.Fatalf("denied or malformed comparison reached the reader: snapshots=%d", catalog.snapshots)
	}
	get(current+"/schema-diff?desired_database_id="+string(explorerDesired), 200,
		`"current_schema":"public"`, `"desired_schema":"public"`, `"action":"add"`, `CREATE TABLE public.widgets`, `"destructive_count":0`)
	if catalog.snapshots != 2 {
		t.Fatalf("expected two authorized snapshots, got %d", catalog.snapshots)
	}
}
