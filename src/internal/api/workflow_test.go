package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var _ migration.WorkflowRepository = (*postgres.M2Store)(nil)

type workflowFixture struct {
	migration.WorkflowRepository
	migration.WorkflowTx
	definition migration.MigrationDefinition
	change     migration.ChangeRequest
	job        migration.JobView
	groups     []migration.RequiredApproval
}

func (*workflowFixture) Engine(context.Context, security.ID) (dbms.Engine, error) {
	return dbms.PostgreSQL, nil
}

func (f *workflowFixture) WithWorkflow(_ context.Context, _ migration.Request, fn func(migration.WorkflowTx) error) error {
	return fn(f)
}

func (*workflowFixture) RequireAccess(security.DatabaseAccess) error { return nil }

func (*workflowFixture) Position() (migration.Position, error) {
	return migration.Position{Version: 1}, nil
}

func (f *workflowFixture) RequiredApprovers() ([]migration.RequiredApproval, error) {
	return f.groups, nil
}

func (f *workflowFixture) CreateDefinition(d migration.MigrationDefinition) error {
	f.definition = d
	return nil
}

func (f *workflowFixture) UpdateDefinition(id security.ID, revision uint64, input migration.DefinitionInput, checksum migration.DefinitionChecksum, at time.Time) (migration.MigrationDefinition, error) {
	d := &f.definition
	if d.ID != id || d.State != migration.DefinitionDraft || d.Revision != revision {
		return migration.MigrationDefinition{}, security.ErrConflict
	}
	d.DefinitionInput, d.Checksum, d.UpdatedAt = input, checksum, at
	d.Revision++
	return *d, nil
}

func (f *workflowFixture) Definition(id security.ID) (migration.MigrationDefinition, error) {
	return f.FindDefinition(context.Background(), testDatabase, id)
}

func (f *workflowFixture) FindDefinition(_ context.Context, db, id security.ID) (migration.MigrationDefinition, error) {
	if id != f.definition.ID || db != f.definition.DatabaseID {
		return migration.MigrationDefinition{}, security.ErrNotFound
	}
	return f.definition, nil
}

func (f *workflowFixture) ListDefinitions(_ context.Context, _ security.ID, query migration.PageQuery) (migration.Page[migration.MigrationDefinitionSummary], error) {
	d := f.definition
	return migration.Page[migration.MigrationDefinitionSummary]{
		Page: query.Page, Limit: query.Limit, Items: []migration.MigrationDefinitionSummary{
			{ID: d.ID, DatabaseID: d.DatabaseID, Title: d.Title, State: d.State, Revision: d.Revision,
				CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, CreatedBy: d.CreatedBy, Checksum: d.Checksum, Mode: d.Mode},
		},
	}, nil
}

func (f *workflowFixture) CreateChangeRequest(change migration.ChangeRequest) error {
	f.change, f.definition.State = change, migration.DefinitionFrozen
	return nil
}

func (f *workflowFixture) ChangeRequest(id security.ID) (migration.ChangeRequest, error) {
	return f.FindChangeRequest(context.Background(), testDatabase, id)
}

func (f *workflowFixture) FindChangeRequest(_ context.Context, db, id security.ID) (migration.ChangeRequest, error) {
	if id != f.change.ID || db != f.change.DatabaseID {
		return migration.ChangeRequest{}, security.ErrNotFound
	}
	return f.change, nil
}

func (*workflowFixture) ListChangeRequests(_ context.Context, _ security.ID, query migration.ChangeRequestQuery) (migration.Page[migration.ChangeRequest], error) {
	return migration.Page[migration.ChangeRequest]{Page: query.Page, Limit: query.Limit}, nil
}

func (f *workflowFixture) RecordDecision(_ security.ID, revision uint64, decision migration.ApprovalDecision) (migration.ChangeRequest, error) {
	var approval *migration.GroupApproval
	for i := range f.change.Approvals {
		if f.change.Approvals[i].GroupID == decision.GroupID {
			approval = &f.change.Approvals[i]
			break
		}
	}
	if approval.Decision != nil {
		if approval.Decision.Decision == decision.Decision && approval.Decision.Note == decision.Note {
			return f.change, nil
		}
		return migration.ChangeRequest{}, security.ErrConflict
	}
	if f.change.Status != migration.CRPendingReview || f.change.Revision != revision {
		return migration.ChangeRequest{}, security.ErrConflict
	}
	approval.Decision = &decision
	f.change.Revision++
	if decision.Decision == migration.DecisionReject {
		f.change.Status = migration.CRRejected
	} else {
		approved := true
		for _, group := range f.change.Approvals {
			approved = approved && group.Decision != nil
		}
		if approved {
			f.change.Status = migration.CRApproved
		}
	}
	return f.change, nil
}

func (f *workflowFixture) EnqueueApproved(_ security.ID, _ uint64, spec migration.JobSpec) (migration.JobView, error) {
	if f.job.ID == "" {
		spec.TraceContext = security.NewSecret("internal carrier")
		f.job = migration.JobView{JobSpec: spec, MigrationID: f.change.MigrationID, Kind: f.change.Kind,
			Mode: f.definition.Mode, Status: migration.JobQueued, RunAfter: spec.QueuedAt}
		f.change.Status = migration.CRQueued
		f.change.Revision++
	}
	return f.job, nil
}

func (f *workflowFixture) JobDatabase(_ context.Context, id security.ID) (security.ID, error) {
	if id != f.job.ID {
		return "", security.ErrNotFound
	}
	return f.job.DatabaseID, nil
}

func (f *workflowFixture) FindJob(_ context.Context, _, _ security.ID) (migration.JobView, error) {
	return f.job, nil
}

func newWorkflowTestClient(t *testing.T, actor *integrationFixture, store migration.WorkflowRepository, engine migration.WorkflowEngine) func(string, string, string, int) *httptest.ResponseRecorder {
	t.Helper()
	service, err := migration.NewWorkflowService(migration.WorkflowConfig{Store: store, Authorizer: actor.authorizer,
		Engines: engine, IDs: security.NewTokenGenerator(), Clock: actor.clock})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := WorkflowRoutes(service)
	if err != nil {
		t.Fatal(err)
	}
	handler := buildAPI(t, actor, routes...)
	session := fixtureLogin(t, actor)
	return func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		got := dispatch(handler, request(method, path, body, session.CookieToken, session.CSRFToken))
		if got.Code != status || got.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, got.Code, status, got.Body)
		}
		return got
	}
}

func TestWorkflowRoutesDraftToQueuedJob(t *testing.T) {
	store := &workflowFixture{}
	editor := newFixture(t)
	editor.store.access = security.AccessViewEdit
	groupLeader := newFixture(t)
	groupLeader.store.account.Account.ID = adminTestMember
	adminWithoutGrant := newFixture(t)
	adminWithoutGrant.store.account.Account.PlatformAdmin = true
	adminWithoutGrant.store.access = security.AccessNone
	send := newWorkflowTestClient(t, editor, store, store)
	leader := newWorkflowTestClient(t, groupLeader, store, store)
	admin := newWorkflowTestClient(t, adminWithoutGrant, store, store)
	base := Prefix + "/databases/" + string(testDatabase)
	const draft = `{"title":"Add table","up_sql":"CREATE TABLE widgets (id int)","down_sql":"DROP TABLE widgets","irreversible":false,"irreversible_reason":"","mode":"normal","batch":null}`
	leader("POST", base+"/migrations", draft, 403)
	admin("POST", base+"/migrations", draft, 403)
	send("POST", base+"/migrations", strings.Replace(draft, `,"batch":null`, "", 1), 400)
	created := send("POST", base+"/migrations", draft, 201)
	migrationPath := created.Header().Get("Location")
	send("PATCH", migrationPath, strings.TrimSuffix(draft, "}")+`,"revision":1}`, 200)
	if !strings.Contains(send("GET", migrationPath, "", 200).Body.String(), `"revision":2`) {
		t.Fatal("updated migration revision missing")
	}
	if strings.Contains(send("GET", base+"/migrations", "", 200).Body.String(), "up_sql") {
		t.Fatal("migration summary exposed SQL")
	}
	submit := `{"migration_id":"` + string(store.definition.ID) + `","migration_revision":2,"kind":"apply"}`
	send("POST", base+"/change-requests", submit, 422)
	store.groups = []migration.RequiredApproval{{GroupID: adminTestGroup, GroupName: "Review", Access: security.AccessViewOnly}}
	send("POST", base+"/change-requests", submit, 422)
	store.groups[0].LeaderUserID = adminTestMember
	secondGroup := security.ID("40000000-0000-4000-8000-000000000002")
	store.groups = append(store.groups, migration.RequiredApproval{
		GroupID: secondGroup, GroupName: "Write", Access: security.AccessViewEdit, LeaderUserID: adminTestMember,
	})
	change := send("POST", base+"/change-requests", submit, 201)
	changePath := change.Header().Get("Location")
	send("GET", changePath, "", 200)
	send("GET", base+"/change-requests?status=pending_review", "", 200)
	send("PATCH", migrationPath, strings.TrimSuffix(draft, "}")+`,"revision":2}`, 409)
	send("POST", changePath+"/execute", `{"revision":1}`, 409)
	vote := `{"group_id":"` + string(adminTestGroup) + `","revision":1,"decision":"approve"}`
	send("POST", changePath+"/approvals", vote, 403)
	partiallyApproved := leader("POST", changePath+"/approvals", vote, 200)
	if !strings.Contains(partiallyApproved.Body.String(), `"status":"pending_review"`) {
		t.Fatal("one decision must not cover both groups led by the same user")
	}
	send("POST", changePath+"/execute", `{"revision":2}`, 409)
	secondVote := `{"group_id":"` + string(secondGroup) + `","revision":2,"decision":"approve"}`
	approved := leader("POST", changePath+"/approvals", secondVote, 200)
	if !strings.Contains(approved.Body.String(), `"status":"approved"`) {
		t.Fatal("leader approval did not move request to approved")
	}
	leader("POST", changePath+"/approvals", vote, 200)
	leader("POST", changePath+"/approvals", strings.Replace(vote, "approve", "reject", 1), 409)
	send("POST", changePath+"/execute", `{"revision":1}`, 409)
	store.groups[0].LeaderUserID = testUser
	send("POST", changePath+"/execute", `{"revision":3}`, 409)
	leader("POST", changePath+"/approvals", vote, 403)
	store.groups[0].LeaderUserID = adminTestMember
	queued := send("POST", changePath+"/execute", `{"revision":3}`, 202)
	jobPath := queued.Header().Get("Location")
	if jobPath != Prefix+"/jobs/"+string(store.job.ID) || store.job.RequestID == "" {
		t.Fatal("execute did not return job location and correlate queue request")
	}
	store.job.Status, store.change.Status = migration.JobSucceeded, migration.CRSucceeded
	replay := send("POST", changePath+"/execute", `{"revision":3}`, 202)
	if replay.Header().Get("Location") != jobPath || !strings.Contains(replay.Body.String(), `"status":"succeeded"`) {
		t.Fatal("exact replay did not return the existing job and current status")
	}
	send("POST", changePath+"/execute", `{"revision":4}`, 409)
	admin("GET", jobPath, "", 403)
	job := leader("GET", jobPath, "", 200)
	var record map[string]json.RawMessage
	if err := json.Unmarshal(job.Body.Bytes(), &record); err != nil || record["progress"] == nil || record["trace_context"] != nil {
		t.Fatal("job response did not project only public metadata")
	}

	send("POST", base+"/migrations", draft, 201)
	submit = `{"migration_id":"` + string(store.definition.ID) + `","migration_revision":1,"kind":"apply"}`
	review := send("POST", base+"/change-requests", submit, 201).Header().Get("Location")
	rejected := leader("POST", review+"/approvals", strings.Replace(vote, "approve", "reject", 1), 200)
	if !strings.Contains(rejected.Body.String(), `"status":"rejected"`) {
		t.Fatal("one rejection must close the request")
	}
	send("POST", review+"/execute", `{"revision":2}`, 409)
}
