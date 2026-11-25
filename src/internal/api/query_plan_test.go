package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const planMigrationID security.ID = "20000000-0000-4000-8000-000000000001"

type queryPlanFixture struct {
	definition   migration.MigrationDefinition
	reads        int
	locks        int
	queries      []migration.PlanQuery
	afterExplain func()
}

func (f *queryPlanFixture) FindDefinition(_ context.Context, databaseID, id security.ID) (migration.MigrationDefinition, error) {
	f.reads++
	if databaseID != f.definition.DatabaseID || id != f.definition.ID {
		return migration.MigrationDefinition{}, security.ErrNotFound
	}
	return f.definition, nil
}

func (*queryPlanFixture) ListDefinitions(context.Context, security.ID, migration.PageQuery) (migration.Page[migration.MigrationDefinitionSummary], error) {
	return migration.Page[migration.MigrationDefinitionSummary]{}, security.ErrUnavailable
}

func (f *queryPlanFixture) WithPlanLock(ctx context.Context, _ security.ID, run func(context.Context, migration.PlanTarget) error) error {
	f.locks++
	return run(ctx, f)
}

func (f *queryPlanFixture) Explain(_ context.Context, query migration.PlanQuery) ([]byte, error) {
	f.queries = append(f.queries, query)
	if f.afterExplain != nil {
		f.afterExplain()
	}
	if query.Analyze {
		return []byte(`[{"Plan":{"Node Type":"Result","Actual Startup Time":0,"Actual Total Time":0.1,"Actual Rows":1,"Actual Loops":1},"Planning Time":1,"Execution Time":2}]`), nil
	}
	return []byte(`[{"Plan":{"Node Type":"Append","Total Cost":1,"Plans":[{"Node Type":"Result","Plan Rows":1,"Filter":"test private expression"}]}}]`), nil
}

func TestQueryPlanUsesSavedDefinitionAndChecksAuthorization(t *testing.T) {
	f := newFixture(t)
	session := fixtureLogin(t, f)
	plan := &queryPlanFixture{definition: migration.MigrationDefinition{
		ID: planMigrationID, DatabaseID: testDatabase, Revision: 3, State: migration.DefinitionFrozen,
		DefinitionInput: migration.DefinitionInput{
			Title: "Plan", Mode: migration.ModeNormal,
			UpSQL: security.NewSecret("SELECT 1; SELECT 2;"), DownSQL: security.NewSecret("DELETE FROM widgets;"),
		},
	}}
	plan.definition.Checksum = migration.ChecksumDefinition(plan.definition.DefinitionInput)
	service, err := migration.NewDefinitionPlanService(migration.DefinitionPlanConfig{
		Definitions: plan, Authorizer: f.authorizer, Coordinator: plan, Validator: migration.NewPostgreSQLValidator(),
	})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := QueryPlanRoutes(service)
	if err != nil {
		t.Fatal(err)
	}
	handler := buildAPI(t, f, routes...)
	path := Prefix + "/databases/" + string(testDatabase) + "/migrations/" + string(planMigrationID) + "/plan"
	post := func(body string, csrf security.Secret, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := dispatch(handler, request(http.MethodPost, path, body, session.CookieToken, csrf))
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("plan: status=%d want=%d body=%s", w.Code, status, w.Body)
		}
		return w
	}
	const up = `{"revision":3,"direction":"up","statement":2,"analyze":false}`
	post(up, session.CSRFToken, http.StatusForbidden)
	f.store.access = security.AccessViewEdit
	post(up, security.Secret{}, http.StatusForbidden)
	post(`{"revision":3,"direction":"up","statement":1,"analyze":false,"sql":"SELECT 4"}`, session.CSRFToken, http.StatusBadRequest)
	post(`{"revision":3,"direction":"up","statement":1}`, session.CSRFToken, http.StatusUnprocessableEntity)
	if plan.reads != 0 || plan.locks != 0 {
		t.Fatal("a denied or malformed request accessed the catalog or target")
	}
	otherDB := security.ID("10000000-0000-4000-8000-000000000002")
	f.store.additionalAccess = map[security.ID]security.DatabaseAccess{otherDB: security.AccessViewEdit}
	otherPath := strings.Replace(path, string(testDatabase), string(otherDB), 1)
	other := dispatch(handler, request(http.MethodPost, otherPath, up, session.CookieToken, session.CSRFToken))
	if other.Code != http.StatusNotFound {
		t.Fatalf("a definition from another database returned status %d", other.Code)
	}
	post(`{"revision":2,"direction":"up","statement":1,"analyze":false}`, session.CSRFToken, http.StatusConflict)
	post(`{"revision":3,"direction":"down","statement":1,"analyze":true}`, session.CSRFToken, http.StatusUnprocessableEntity)
	if len(plan.queries) != 0 {
		t.Fatal("a stale revision or mutating ANALYZE reached EXPLAIN")
	}

	w := post(up, session.CSRFToken, http.StatusOK)
	var result queryPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Checksum != hex.EncodeToString(plan.definition.Checksum[:]) || result.Revision != 3 ||
		result.Direction != migration.DirectionUp || result.Statement != 2 || result.Analyze ||
		result.Plan.NodeType != "Append" || len(result.Plan.Plans) != 1 || result.Plan.Plans[0].NodeType != "Result" {
		t.Fatal("expected the saved definition checksum and normalized plan tree")
	}
	for _, fragment := range []string{`"planning_ms":null`, `"execution_ms":null`} {
		if !strings.Contains(w.Body.String(), fragment) {
			t.Errorf("missing required field %s", fragment)
		}
	}
	for _, forbidden := range []string{"test private expression", "Node Type", "SELECT 2"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Error("plan response leaked a raw expression or SQL")
		}
	}
	post(`{"revision":3,"direction":"down","statement":1,"analyze":false}`, session.CSRFToken, http.StatusOK)
	actual := post(`{"revision":3,"direction":"up","statement":1,"analyze":true}`, session.CSRFToken, http.StatusOK)
	if len(plan.queries) != 3 || strings.Trim(strings.TrimSpace(plan.queries[0].SQL.Reveal()), ";") != "SELECT 2" ||
		!strings.HasPrefix(plan.queries[1].SQL.Reveal(), "DELETE FROM widgets") || plan.queries[1].Analyze || !plan.queries[2].Analyze {
		t.Fatal("EXPLAIN did not receive the requested saved statements and mode")
	}
	if !strings.Contains(actual.Body.String(), `"actual_startup_ms":0`) || !strings.Contains(actual.Body.String(), `"execution_ms":2`) {
		t.Fatal("ANALYZE response did not include measurements")
	}

	plan.afterExplain = func() { plan.definition.Revision++ }
	post(up, session.CSRFToken, http.StatusConflict)
	plan.afterExplain = func() { f.store.access = security.AccessViewOnly }
	post(`{"revision":4,"direction":"up","statement":1,"analyze":false}`, session.CSRFToken, http.StatusForbidden)
	plan.afterExplain = nil
	f.store.access = security.AccessViewEdit
	plan.definition.Mode = migration.ModeBatch
	plan.definition.Checksum = migration.ChecksumDefinition(plan.definition.DefinitionInput)
	post(`{"revision":4,"direction":"up","statement":1,"analyze":false}`, session.CSRFToken, http.StatusUnprocessableEntity)
	plan.definition.Mode = migration.ModeNormal
	plan.definition.Irreversible = true
	plan.definition.DownSQL = security.Secret{}
	plan.definition.Checksum = migration.ChecksumDefinition(plan.definition.DefinitionInput)
	post(`{"revision":4,"direction":"down","statement":1,"analyze":false}`, session.CSRFToken, http.StatusUnprocessableEntity)
	if len(plan.queries) != 5 {
		t.Fatal("unsupported definition reached EXPLAIN")
	}
}
