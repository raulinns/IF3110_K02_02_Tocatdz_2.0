package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type attemptHistoryFixture struct {
	*workflowFixture
	entry migration.HistoryEntry
}

func (f *attemptHistoryFixture) ListHistory(_ context.Context, db security.ID, q migration.ExecutionQuery) (migration.Page[migration.HistoryEntry], error) {
	e := f.entry
	e.Definition = nil
	return migration.Page[migration.HistoryEntry]{Items: []migration.HistoryEntry{e}, Page: q.Page, Limit: q.Limit}, nil
}
func (f *attemptHistoryFixture) FindHistory(_ context.Context, db, id security.ID) (migration.HistoryEntry, error) {
	if db != f.entry.DatabaseID || id != f.entry.ID {
		return migration.HistoryEntry{}, security.ErrNotFound
	}
	return f.entry, nil
}
func (f *attemptHistoryFixture) HistoryStatements(_ context.Context, db, id security.ID, q migration.PageQuery) (migration.Page[migration.HistoryStatement], error) {
	if _, err := f.FindHistory(context.Background(), db, id); err != nil {
		return migration.Page[migration.HistoryStatement]{}, err
	}
	return migration.Page[migration.HistoryStatement]{Items: []migration.HistoryStatement{{ExecutedStatement: migration.ExecutedStatement{Query: security.NewSecret("SELECT 'private-canary'"), Params: []security.Secret{security.NewSecret("private-param")}}, Status: "failed", SQLState: "22012", StartedAt: f.entry.StartedAt}}, Page: q.Page, Limit: q.Limit}, nil
}

func TestAttemptHistoryRoutesAuthorizeScopeAndRedactStatements(t *testing.T) {
	actor := newFixture(t)
	store := &attemptHistoryFixture{workflowFixture: &workflowFixture{}, entry: migration.HistoryEntry{
		ID: historyID(88), DatabaseID: testDatabase, MigrationID: historyID(89), ExecutionID: historyID(90), JobID: historyID(91),
		Attempt: 1, Revision: 1, Status: migration.JobFailed, ResolvedStatus: migration.JobFailed, Direction: migration.DirectionUp, StartedAt: time.Now().UTC(),
		SnapshotAvailable: true, Definition: &migration.DefinitionInput{UpSQL: security.NewSecret("SELECT 'private-canary'")},
	}}
	send := newWorkflowTestClient(t, actor, store, store)
	base := Prefix + "/databases/" + string(testDatabase) + "/history"
	list := send("GET", base, "", 200).Body.String()
	if strings.Contains(list, "private-canary") || !strings.Contains(list, `"status":"failed"`) || !strings.Contains(list, `"position_version":null`) {
		t.Fatal("invalid summary")
	}
	detail := send("GET", base+"/"+string(store.entry.ID), "", 200).Body.String()
	if !strings.Contains(detail, "private-canary") {
		t.Fatal("authorized detail lost immutable SQL")
	}
	statements := send("GET", base+"/"+string(store.entry.ID)+"/statements", "", 200).Body.String()
	if strings.Contains(statements, "private-canary") || strings.Contains(statements, "private-param") || !strings.Contains(statements, "[REDACTED]") {
		t.Fatal("statement values leaked")
	}
	send("GET", base+"/"+string(historyID(92)), "", 404)
	send("GET", base+"?hide_rollback=invalid", "", 400)
	send("GET", base+"?unexpected=true", "", 400)
	retry := Prefix + "/databases/" + string(testDatabase) + "/jobs/" + string(store.entry.JobID) + "/retry"
	send("POST", retry, `{}`, 403)
	actor.store.access = security.AccessViewEdit
	send("POST", retry, `{"up_sql":"SELECT 2"}`, 400)
	actor.store.access = security.AccessNone
	send("GET", base, "", 403)
	send("GET", base+"/"+string(store.entry.ID), "", 403)
}
