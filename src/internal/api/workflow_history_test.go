package api

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/pgtest"
)

type historyWorkflowFixture struct {
	*workflowFixture
	position   migration.Position
	executions map[security.ID]migration.Execution
}

func (f *historyWorkflowFixture) WithWorkflow(_ context.Context, _ migration.Request, fn func(migration.WorkflowTx) error) error {
	return fn(f)
}

func (f *historyWorkflowFixture) Position() (migration.Position, error) { return f.position, nil }

func (f *historyWorkflowFixture) ReadPosition(context.Context, security.ID) (migration.Position, error) {
	return f.position, nil
}

func (f *historyWorkflowFixture) Execution(id security.ID) (migration.Execution, error) {
	return f.FindExecution(context.Background(), testDatabase, id)
}

func (f *historyWorkflowFixture) FindExecution(_ context.Context, databaseID, id security.ID) (migration.Execution, error) {
	execution, ok := f.executions[id]
	if !ok || execution.DatabaseID != databaseID {
		return migration.Execution{}, security.ErrNotFound
	}
	return execution, nil
}

func (*historyWorkflowFixture) ListJobs(_ context.Context, _ security.ID, query migration.JobQuery) (migration.Page[migration.JobView], error) {
	return migration.Page[migration.JobView]{Page: query.Page, Limit: query.Limit}, nil
}

func (*historyWorkflowFixture) ListAttempts(_ context.Context, _, _ security.ID, page migration.PageQuery) (migration.Page[migration.JobAttempt], error) {
	return migration.Page[migration.JobAttempt]{Page: page.Page, Limit: page.Limit,
		Items: []migration.JobAttempt{{Attempt: 1, Status: migration.JobFailed, Error: &migration.JobFailure{Code: migration.JobErrorSQL, SQLState: "22012"}}},
	}, nil
}

func historyID(n int) security.ID {
	return security.ID(fmt.Sprintf("70000000-0000-4000-8000-%012d", n))
}

func TestWorkflowHistoryOnlyAcceptsLinearTargets(t *testing.T) {
	actor := newFixture(t)
	actor.store.access = security.AccessViewEdit
	store := &historyWorkflowFixture{workflowFixture: &workflowFixture{}, executions: make(map[security.ID]migration.Execution)}
	store.groups = []migration.RequiredApproval{{GroupID: adminTestGroup, LeaderUserID: adminTestMember, Access: security.AccessViewEdit}}
	send := newWorkflowTestClient(t, actor, store, store)
	base := Prefix + "/databases/" + string(testDatabase)
	var chain []migration.CommittedExecutionSummary
	for i := 1; i <= 3; i++ {
		id, definitionID := historyID(i), historyID(i+100)
		d := migration.DefinitionInput{Title: fmt.Sprintf("Migration %d", i), Mode: migration.ModeNormal,
			UpSQL: security.NewSecret("SELECT 1"), DownSQL: security.NewSecret("SELECT 2")}
		hash := migration.ChecksumDefinition(d)
		store.executions[id] = migration.Execution{ID: id, DatabaseID: testDatabase, MigrationID: definitionID, Direction: migration.DirectionUp,
			MigrationRevision: 2, Checksum: hash, Definition: d}
		chain = append(chain, migration.CommittedExecutionSummary{ID: id, DatabaseID: testDatabase, MigrationID: definitionID,
			Direction: migration.DirectionUp, MigrationRevision: 2, Checksum: hash})
	}
	store.position = migration.Position{Version: 3, Active: chain}
	submit := func(kind string, target security.ID, status int) {
		e := store.executions[target]
		store.definition = migration.MigrationDefinition{ID: e.MigrationID, DatabaseID: testDatabase, State: migration.DefinitionFrozen,
			Revision: e.MigrationRevision, Checksum: e.Checksum, DefinitionInput: e.Definition}
		body := fmt.Sprintf(`{"migration_id":"%s","migration_revision":2,"kind":"%s","target_execution_id":"%s"}`, e.MigrationID, kind, target)
		send("POST", base+"/change-requests", body, status)
	}
	submit("rollback", historyID(2), 409)
	submit("rollback", historyID(3), 201)
	if store.change.TargetExecutionID != historyID(3) || store.change.ExpectedPosition != 3 || store.change.Checksum != chain[2].Checksum {
		t.Fatal("rollback must snapshot the active execution and original revision")
	}
	store.position = migration.Position{Version: 5, Active: chain[:1], Forward: chain[1:]}
	submit("forward", historyID(3), 409)
	submit("forward", historyID(2), 201)
	pos := send("GET", base+"/position", "", 200)
	if !strings.Contains(pos.Body.String(), `"can_forward":true`) || !strings.Contains(pos.Body.String(), string(historyID(2))) {
		t.Fatal("position did not select the first forward target")
	}
	store.position.BlockedByJobID = historyID(12)
	submit("forward", historyID(2), 409)
	pos = send("GET", base+"/position", "", 200)
	if !strings.Contains(pos.Body.String(), `"can_forward":false`) || !strings.Contains(pos.Body.String(), `"can_rollback":false`) {
		t.Fatal("position exposed mutations while the database was blocked")
	}
	store.position.BlockedByJobID = ""
	e := store.executions[historyID(1)]
	e.Definition.DownSQL = security.Secret{}
	e.Definition.Irreversible, e.Definition.IrreversibleReason = true, "No inverse available"
	e.Checksum = migration.ChecksumDefinition(e.Definition)
	store.position.Active[0].Checksum = e.Checksum
	store.executions[e.ID] = e
	submit("rollback", e.ID, 422)
	actor.store.access = security.AccessViewOnly
	submit("forward", historyID(2), 403)
	send("GET", base+"/position", "", 200)
	send("GET", base+"/jobs?status=failed", "", 200)
	send("GET", base+"/jobs?status=unknown", "", 400)
	store.job.JobSpec = migration.JobSpec{ID: historyID(12), DatabaseID: testDatabase}
	send("GET", Prefix+"/jobs/"+string(store.job.ID)+"/attempts", "", 200)
	actor.store.access = security.AccessNone
	send("GET", Prefix+"/jobs/"+string(store.job.ID)+"/attempts", "", 403)
}

func TestWorkflowHistoryFiltersBeforePaginationWithPostgreSQL(t *testing.T) {
	dsn := pgtest.Database(t, "TEST_DATABASE_URL")
	ctx := context.Background()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users (id, username, password_hash) VALUES ($1, 'viewer', 'fixture')", string(testUser))
	exec("INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, 'History test', decode('abcd', 'hex'))", string(testDatabase))
	for i := 1; i <= 3; i++ {
		exec(`INSERT INTO migrations (id, database_id, title, sql, status, revision, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, 'SELECT 1', 'draft', 1, $4, now(), now())`, string(historyID(100+i)), string(testDatabase), fmt.Sprint(i), string(testUser))
	}
	var entries = []struct {
		mig                  int
		operation, direction string
		reverts, forward     any
	}{
		{1, "apply", "up", nil, nil}, {2, "apply", "up", nil, nil}, {2, "rollback", "down", string(historyID(2)), nil},
		{2, "forward", "up", nil, string(historyID(2))}, {3, "apply", "up", nil, nil},
	}
	for i, e := range entries {
		at := time.Unix(int64(i+1), 0).UTC()
		exec(`INSERT INTO executions (id, database_id, migration_id, backfilled, migration_revision, operation, direction,
			reverts_execution_id, forward_of_execution_id, title, up_sql, down_sql, irreversible, irreversible_reason, mode,
			statements, actor_id, actor, started_at, committed_at, position_version)
			VALUES ($1,$2,$3,true,1,$4,$5,$6,$7,'Fixture','SELECT 1','SELECT 2',false,'','normal',
			'[{"query":"SELECT 987, $1, ''private literal''","params":["private parameter"],"batch":null}]',
			$8,'viewer',$9,$9,$10)`, string(historyID(i+1)), string(testDatabase), string(historyID(100+e.mig)),
			e.operation, e.direction, e.reverts, e.forward, string(testUser), at, i+1)
	}
	exec("UPDATE database_workflows SET position_version = 5 WHERE database_id = $1", string(testDatabase))
	store, err := postgres.NewM2Store(pool)
	if err != nil {
		t.Fatal(err)
	}
	actor := newFixture(t)
	send := newWorkflowTestClient(t, actor, store, &workflowFixture{})
	base := Prefix + "/databases/" + string(testDatabase) + "/executions"
	readPage := func(query string, wantIDs []security.ID, hasNext bool) {
		t.Helper()
		var page struct {
			Items   []workflowExecutionSummary `json:"items"`
			HasNext bool                       `json:"has_next"`
		}
		w := send("GET", base+query, "", 200)
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		var got []security.ID
		for _, item := range page.Items {
			got = append(got, item.ID)
		}
		if !slices.Equal(got, wantIDs) || page.HasNext != hasNext || strings.Contains(w.Body.String(), "sql_snapshot") {
			t.Fatalf("unexpected history page: %v has_next=%t", got, page.HasNext)
		}
	}
	readPage("?hide_rollback=false", []security.ID{historyID(5), historyID(4), historyID(3), historyID(2), historyID(1)}, false)
	readPage("?hide_rollback=true&limit=2", []security.ID{historyID(5), historyID(4)}, true)
	readPage("?hide_rollback=true&limit=2&page=2", []security.ID{historyID(1)}, false)
	send("GET", base+"?hide_rollback=1", "", 400)
	rollback := base + "/" + string(historyID(3))
	if !strings.Contains(send("GET", rollback, "", 200).Body.String(), `"sql_snapshot":"SELECT 2"`) {
		t.Fatal("rollback detail did not return the committed down snapshot")
	}
	statements := send("GET", rollback+"/statements", "", 200).Body.String()
	if !strings.Contains(statements, `"query":"SELECT $2, $1, $3"`) ||
		!strings.Contains(statements, `"params":["[REDACTED]"]`) || strings.Contains(statements, "private") {
		t.Fatal("statement projection did not redact literals and parameters")
	}
	// Existing committed history without job_attempts remains visible after upgrade.
	historyBase := Prefix + "/databases/" + string(testDatabase) + "/history"
	var attempts struct {
		Items   []historyResponse `json:"items"`
		HasNext bool              `json:"has_next"`
	}
	response := send("GET", historyBase+"?hide_rollback=true&limit=2", "", 200)
	if err := json.Unmarshal(response.Body.Bytes(), &attempts); err != nil {
		t.Fatal(err)
	}
	if len(attempts.Items) != 2 || attempts.Items[0].Status != migration.JobSucceeded || attempts.Items[0].ExecutionID != historyID(5) || attempts.Items[1].ExecutionID != historyID(4) || !attempts.HasNext {
		t.Fatal("legacy committed history disappeared")
	}
	detail := send("GET", historyBase+"/"+string(historyID(3)), "", 200).Body.String()
	if !strings.Contains(detail, `"sql_snapshot":"SELECT 2"`) || !strings.Contains(detail, `"attempt":0`) {
		t.Fatal("legacy immutable snapshot unavailable")
	}
	legacyStatements := send("GET", historyBase+"/"+string(historyID(3))+"/statements", "", 200).Body.String()
	if strings.Contains(legacyStatements, "private") || !strings.Contains(legacyStatements, `"status":"succeeded"`) {
		t.Fatal("legacy statement fallback did not preserve redaction")
	}
}
