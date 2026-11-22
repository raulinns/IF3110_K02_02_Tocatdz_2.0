package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type m2StoreFixture struct {
	*adminFixture
	m2                                 *M2Store
	requester, leaderA, leaderB, other migration.Request
	groupB                             security.ID
	now                                time.Time
}

func principalFor(t *testing.T, f *adminFixture, user security.ID) security.Principal {
	t.Helper()
	return security.Principal{UserID: user, SessionID: liveSession(t, f.pool, user), AuthVersion: 1, CSRFEpoch: 1}
}

func newM2StoreFixture(t *testing.T) *m2StoreFixture {
	t.Helper()
	base := newAdminFixture(t)
	ctx := context.Background()
	store, err := NewM2Store(base.pool)
	if err != nil {
		t.Fatal(err)
	}
	f := &m2StoreFixture{adminFixture: base, m2: store, now: time.Now().UTC().Truncate(time.Second)}
	leaderA, leaderB, outsider := insertUser(t, base.pool, "leader_a", false), insertUser(t, base.pool, "leader_b", false), insertUser(t, base.pool, "outsider", false)
	f.groupB = newID(t)
	must(t, base.store.CreateGroup(ctx, base.actor, admin.Group{ID: f.groupB, Name: "zeta"}))
	must(t, base.store.AddMember(ctx, base.actor, base.group, "siti"))
	must(t, base.store.AddMember(ctx, base.actor, base.group, "leader_a"))
	must(t, base.store.AddMember(ctx, base.actor, f.groupB, "leader_b"))
	must(t, base.store.SetGroupLeader(ctx, base.actor, base.group, leaderA))
	must(t, base.store.SetGroupLeader(ctx, base.actor, f.groupB, leaderB))
	must(t, base.store.SetGrant(ctx, base.actor, base.group, base.db, security.AccessViewEdit))
	must(t, base.store.SetGrant(ctx, base.actor, f.groupB, base.db, security.AccessViewOnly))
	request := func(user security.ID) migration.Request {
		var version int64
		if err := base.pool.QueryRow(ctx, "SELECT auth_version FROM users WHERE id = $1", string(user)).Scan(&version); err != nil {
			t.Fatal(err)
		}
		principal := principalFor(t, base, user)
		principal.AuthVersion = uint64(version)
		return migration.Request{Principal: principal, DatabaseID: base.db}
	}
	f.requester, f.leaderA, f.leaderB, f.other = request(base.member), request(leaderA), request(leaderB), request(outsider)
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func expectErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func checksum(fill byte) migration.DefinitionChecksum {
	var value migration.DefinitionChecksum
	for i := range value {
		value[i] = fill
	}
	return value
}

func (f *m2StoreFixture) workflow(t *testing.T, request migration.Request, fn func(migration.WorkflowTx) error) error {
	t.Helper()
	return f.m2.WithWorkflow(context.Background(), request, fn)
}

func (f *m2StoreFixture) createDefinition(t *testing.T) security.ID {
	t.Helper()
	id := newID(t)
	must(t, f.workflow(t, f.requester, func(tx migration.WorkflowTx) error {
		if err := tx.RequireAccess(security.AccessViewEdit); err != nil {
			return err
		}
		return tx.CreateDefinition(migration.MigrationDefinition{ID: id, DatabaseID: f.db, Revision: 1, Checksum: checksum(1),
			CreatedBy: f.requester.Principal.UserID, CreatedAt: f.now, UpdatedAt: f.now,
			DefinitionInput: migration.DefinitionInput{Title: "add c", UpSQL: security.NewSecret("ALTER TABLE t ADD c int"),
				DownSQL: security.NewSecret("ALTER TABLE t DROP c"), Mode: migration.ModeNormal}})
	}))
	return id
}

func (f *m2StoreFixture) submit(t *testing.T, migrationID security.ID, revision uint64, sum migration.DefinitionChecksum) (security.ID, error) {
	t.Helper()
	id := newID(t)
	err := f.workflow(t, f.requester, func(tx migration.WorkflowTx) error {
		approvers, err := tx.RequiredApprovers()
		if err != nil {
			return err
		}
		position, err := tx.Position()
		if err != nil {
			return err
		}
		approvals := make([]migration.GroupApproval, len(approvers))
		for i, approver := range approvers {
			approvals[i] = migration.GroupApproval{RequiredApproval: approver}
		}
		return tx.CreateChangeRequest(migration.ChangeRequest{ID: id, DatabaseID: f.db, Approvals: approvals, Checksum: sum,
			SubmitChange: migration.SubmitChange{MigrationID: migrationID, MigrationRevision: revision, Kind: migration.ChangeApply},
			RequestedBy:  f.requester.Principal.UserID, ExpectedPosition: position.Version, CreatedAt: f.now})
	})
	return id, err
}

func (f *m2StoreFixture) decide(t *testing.T, request migration.Request, cr, group security.ID, revision uint64, decision migration.Decision, note string) (migration.ChangeRequest, error) {
	t.Helper()
	var result migration.ChangeRequest
	err := f.workflow(t, request, func(tx migration.WorkflowTx) error {
		var err error
		result, err = tx.RecordDecision(cr, revision, migration.ApprovalDecision{GroupID: group, ActorID: request.Principal.UserID,
			Decision: decision, Note: note, DecidedAt: f.now})
		return err
	})
	return result, err
}

func (f *m2StoreFixture) enqueue(t *testing.T, cr security.ID, revision uint64) (migration.JobView, error) {
	t.Helper()
	var job migration.JobView
	err := f.workflow(t, f.requester, func(tx migration.WorkflowTx) error {
		var err error
		job, err = tx.EnqueueApproved(cr, revision, migration.JobSpec{ID: newID(t), ExecutionID: newID(t), MaxAttempts: 3,
			RequestID: "req-1", TraceContext: security.NewSecret(`{"traceparent":"00-abc-def-01"}`), QueuedAt: f.now})
		return err
	})
	return job, err
}

func TestM2StoreApprovalWorkflowHappyPath(t *testing.T) {
	f := newM2StoreFixture(t)
	ctx := context.Background()
	migrationID := f.createDefinition(t)

	var updated migration.MigrationDefinition
	must(t, f.workflow(t, f.requester, func(tx migration.WorkflowTx) error {
		var err error
		updated, err = tx.UpdateDefinition(migrationID, 1, migration.DefinitionInput{Title: "add c v2", UpSQL: security.NewSecret("ALTER TABLE t ADD c bigint"),
			Irreversible: true, IrreversibleReason: "drops data", Mode: migration.ModeNormal}, checksum(2), f.now)
		return err
	}))
	if updated.Revision != 2 || !updated.DownSQL.Empty() || updated.State != migration.DefinitionDraft {
		t.Fatalf("updated definition = %+v", updated)
	}

	cr, err := f.submit(t, migrationID, 2, checksum(2))
	must(t, err)
	definition, err := f.m2.FindDefinition(ctx, f.db, migrationID)
	must(t, err)
	if definition.State != migration.DefinitionFrozen {
		t.Fatal("submission must freeze the definition")
	}
	err = f.workflow(t, f.requester, func(tx migration.WorkflowTx) error {
		_, err := tx.UpdateDefinition(migrationID, 2, migration.DefinitionInput{Title: "x", UpSQL: security.NewSecret("SELECT 1"),
			Irreversible: true, IrreversibleReason: "x", Mode: migration.ModeNormal}, checksum(3), f.now)
		return err
	})
	expectErr(t, err, security.ErrConflict)

	request, err := f.m2.FindChangeRequest(ctx, f.db, cr)
	must(t, err)
	if request.Status != migration.CRPendingReview || len(request.Approvals) != 2 || request.Revision != 1 {
		t.Fatalf("submitted CR = %+v", request)
	}

	after, err := f.decide(t, f.leaderA, cr, f.group, 1, migration.DecisionApprove, "ok")
	must(t, err)
	if after.Status != migration.CRPendingReview || after.Revision != 2 {
		t.Fatalf("after one approval = %s rev %d", after.Status, after.Revision)
	}
	replay, err := f.decide(t, f.leaderA, cr, f.group, 1, migration.DecisionApprove, "ok")
	must(t, err)
	if replay.Revision != 2 {
		t.Fatalf("replay revision = %d, want 2", replay.Revision)
	}
	_, err = f.decide(t, f.leaderA, cr, f.group, 2, migration.DecisionReject, "changed my mind")
	expectErr(t, err, security.ErrConflict)

	approved, err := f.decide(t, f.leaderB, cr, f.groupB, 2, migration.DecisionApprove, "")
	must(t, err)
	if approved.Status != migration.CRApproved || approved.Revision != 3 {
		t.Fatalf("after all approvals = %s rev %d", approved.Status, approved.Revision)
	}

	job, err := f.enqueue(t, cr, 3)
	must(t, err)
	if job.Status != migration.JobQueued || job.Attempt != 0 || job.MigrationID != migrationID || job.Kind != migration.ChangeApply {
		t.Fatalf("job = %+v", job)
	}
	again, err := f.enqueue(t, cr, 3)
	must(t, err)
	if again.ID != job.ID {
		t.Fatalf("duplicate enqueue created job %s, want %s", again.ID, job.ID)
	}
	queued, err := f.m2.FindChangeRequest(ctx, f.db, cr)
	must(t, err)
	if queued.Status != migration.CRQueued {
		t.Fatalf("CR status after enqueue = %s", queued.Status)
	}

	located, err := f.m2.JobDatabase(ctx, job.ID)
	must(t, err)
	if located != f.db {
		t.Fatalf("job database = %s", located)
	}
	jobs, err := f.m2.ListJobs(ctx, f.db, migration.JobQuery{Status: migration.JobQueued})
	must(t, err)
	if len(jobs.Items) != 1 || jobs.Items[0].TraceContext.Reveal() == "" {
		t.Fatalf("jobs = %+v", jobs)
	}
	attempts, err := f.m2.ListAttempts(ctx, f.db, job.ID, migration.PageQuery{})
	must(t, err)
	if len(attempts.Items) != 0 || attempts.Limit != 50 {
		t.Fatalf("attempts = %+v", attempts)
	}
	list, err := f.m2.ListChangeRequests(ctx, f.db, migration.ChangeRequestQuery{Status: migration.CRQueued})
	must(t, err)
	if len(list.Items) != 1 || len(list.Items[0].Approvals) != 2 {
		t.Fatalf("change requests = %+v", list)
	}
	definitions, err := f.m2.ListDefinitions(ctx, f.db, migration.PageQuery{})
	must(t, err)
	if len(definitions.Items) != 1 || definitions.Items[0].State != migration.DefinitionFrozen {
		t.Fatalf("definitions = %+v", definitions)
	}
}

func TestM2StoreRejectsInvalidAndUnauthorizedActions(t *testing.T) {
	f := newM2StoreFixture(t)
	ctx := context.Background()

	expectErr(t, f.workflow(t, f.other, func(tx migration.WorkflowTx) error { return tx.RequireAccess(security.AccessViewOnly) }), security.ErrForbidden)
	expectErr(t, f.workflow(t, f.leaderB, func(tx migration.WorkflowTx) error { return tx.RequireAccess(security.AccessViewEdit) }), security.ErrForbidden)
	must(t, f.workflow(t, f.leaderB, func(tx migration.WorkflowTx) error { return tx.RequireAccess(security.AccessViewOnly) }))

	migrationID := f.createDefinition(t)
	_, err := f.submit(t, migrationID, 2, checksum(1))
	expectErr(t, err, security.ErrConflict)
	_, err = f.submit(t, migrationID, 1, checksum(9))
	expectErr(t, err, security.ErrConflict)

	cr, err := f.submit(t, migrationID, 1, checksum(1))
	must(t, err)
	_, err = f.submit(t, migrationID, 1, checksum(1))
	expectErr(t, err, security.ErrConflict)

	_, err = f.decide(t, f.requester, cr, f.group, 1, migration.DecisionApprove, "")
	expectErr(t, err, security.ErrForbidden)
	_, err = f.decide(t, f.leaderB, cr, f.group, 1, migration.DecisionApprove, "")
	expectErr(t, err, security.ErrForbidden)
	_, err = f.decide(t, f.other, cr, newID(t), 1, migration.DecisionApprove, "")
	expectErr(t, err, security.ErrForbidden)
	_, err = f.decide(t, f.leaderA, cr, f.group, 7, migration.DecisionApprove, "")
	expectErr(t, err, security.ErrConflict)

	_, err = f.enqueue(t, cr, 1)
	expectErr(t, err, security.ErrConflict)

	rejected, err := f.decide(t, f.leaderB, cr, f.groupB, 1, migration.DecisionReject, "not now")
	must(t, err)
	if rejected.Status != migration.CRRejected {
		t.Fatalf("status after reject = %s", rejected.Status)
	}
	_, err = f.decide(t, f.leaderA, cr, f.group, 2, migration.DecisionApprove, "")
	expectErr(t, err, security.ErrConflict)

	other := insertDatabase(t, f.pool, "other")
	_, err = f.m2.FindChangeRequest(ctx, other, cr)
	expectErr(t, err, security.ErrNotFound)
	_, err = f.m2.FindDefinition(ctx, other, migrationID)
	expectErr(t, err, security.ErrNotFound)

	exec(t, f.pool, "UPDATE sessions SET revoked_at = now() WHERE id = $1", string(f.requester.Principal.SessionID))
	expectErr(t, f.workflow(t, f.requester, func(migration.WorkflowTx) error { return nil }), security.ErrUnauthenticated)
	sentinel := errors.New("boom")
	before, err := f.m2.ListDefinitions(ctx, f.db, migration.PageQuery{})
	must(t, err)
	expectErr(t, f.workflow(t, f.leaderA, func(tx migration.WorkflowTx) error {
		_ = tx.CreateDefinition(migration.MigrationDefinition{ID: newID(t), DatabaseID: f.db, Revision: 1, Checksum: checksum(4),
			CreatedBy: f.leaderA.Principal.UserID, CreatedAt: f.now, UpdatedAt: f.now,
			DefinitionInput: migration.DefinitionInput{Title: "x", UpSQL: security.NewSecret("SELECT 1"), Irreversible: true,
				IrreversibleReason: "x", Mode: migration.ModeNormal}})
		return sentinel
	}), sentinel)
	after, err := f.m2.ListDefinitions(ctx, f.db, migration.PageQuery{})
	must(t, err)
	if len(after.Items) != len(before.Items) {
		t.Fatal("callback error did not roll back")
	}
	_, err = f.m2.ListDefinitions(ctx, f.db, migration.PageQuery{Limit: 201})
	expectErr(t, err, security.ErrInvalidInput)
}

func TestM2StoreGroupWithoutActiveLeaderIsReported(t *testing.T) {
	f := newM2StoreFixture(t)
	ctx := context.Background()
	must(t, f.store.SetGroupLeader(ctx, f.actor, f.groupB, ""))
	approvers, err := f.m2.RequiredApprovers(ctx, f.db)
	must(t, err)
	if len(approvers) != 2 || approvers[0].LeaderUserID == "" || approvers[1].LeaderUserID != "" {
		t.Fatalf("approvers = %+v", approvers)
	}
	leader, err := f.m2.IsLeader(ctx, f.leaderA.Principal.UserID, f.group)
	must(t, err)
	groups, err := f.m2.LeaderGroups(ctx, f.leaderA.Principal.UserID)
	must(t, err)
	if !leader || len(groups) != 1 || groups[0] != f.group {
		t.Fatalf("leader directory: leader=%v groups=%v", leader, groups)
	}
}

func TestM2AdminGroupLeaderRules(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	outsider := insertUser(t, f.pool, "outsider", false)
	expectErr(t, f.store.SetGroupLeader(ctx, f.actor, f.group, outsider), security.ErrInvalidInput)
	expectErr(t, f.store.SetGroupLeader(ctx, f.actor, newID(t), ""), security.ErrNotFound)
	must(t, f.store.AddMember(ctx, f.actor, f.group, "siti"))
	must(t, f.store.SetGroupLeader(ctx, f.actor, f.group, f.member))
	leader, err := f.store.GroupLeader(ctx, f.group)
	must(t, err)
	if leader != f.member {
		t.Fatalf("leader = %s", leader)
	}
	expectErr(t, f.store.RemoveMember(ctx, f.actor, f.group, f.member), security.ErrConflict)
	must(t, f.store.SetGroupLeader(ctx, f.actor, f.group, ""))
	must(t, f.store.RemoveMember(ctx, f.actor, f.group, f.member))
	member := security.Principal{UserID: outsider, SessionID: liveSession(t, f.pool, outsider), AuthVersion: 1, CSRFEpoch: 1}
	expectErr(t, f.store.SetGroupLeader(ctx, member, f.group, ""), security.ErrForbidden)
}

// Contoh spesifikasi: A [E1], B [E2], rollback E2 [E3], forward B [E4], C [E5].
func TestM2StoreHistoryHideRollbackAndPosition(t *testing.T) {
	f := newM2StoreFixture(t)
	ctx := context.Background()
	migrationIDs := map[string]security.ID{}
	for _, name := range []string{"A", "B", "C"} {
		id := newID(t)
		exec(t, f.pool, `INSERT INTO migrations (id, database_id, title, sql, status, revision, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, 'SELECT 1', 'draft', 1, $4, now(), now())`, string(id), string(f.db), name, string(f.member))
		migrationIDs[name] = id
	}
	ids := map[string]security.ID{}
	insert := func(label, migrationName, operation, direction, reverts, forwardOf string, version int) {
		ids[label] = newID(t)
		var revertsID, forwardID any
		if reverts != "" {
			revertsID = string(ids[reverts])
		}
		if forwardOf != "" {
			forwardID = string(ids[forwardOf])
		}
		at := f.now.Add(time.Duration(version) * time.Minute)
		exec(t, f.pool, `INSERT INTO executions (id, database_id, migration_id, backfilled, migration_revision, operation, direction,
			reverts_execution_id, forward_of_execution_id, title, up_sql, down_sql, irreversible, irreversible_reason, mode, statements,
			actor, started_at, committed_at, position_version)
			VALUES ($1, $2, $3, true, 1, $4, $5, $6, $7, $8, 'SELECT 1', 'SELECT 2', false, '', 'normal',
			'[{"query":"SELECT 1","params":[],"batch":null},{"query":"SELECT 2","params":["x"],"batch":3}]', 'siti', $9, $9, $10)`,
			string(ids[label]), string(f.db), string(migrationIDs[migrationName]), operation, direction, revertsID, forwardID, migrationName, at, version)
		exec(t, f.pool, "UPDATE database_workflows SET position_version = $2 WHERE database_id = $1", string(f.db), version)
	}
	insert("E1", "A", "apply", "up", "", "", 1)
	insert("E2", "B", "apply", "up", "", "", 2)
	insert("E3", "B", "rollback", "down", "E2", "", 3)

	position, err := f.m2.ReadPosition(ctx, f.db)
	must(t, err)
	if position.Version != 3 || len(position.Active) != 1 || position.Active[0].ID != ids["E1"] || len(position.Forward) != 1 || position.Forward[0].ID != ids["E2"] {
		t.Fatalf("position after rollback = %+v", position)
	}

	insert("E4", "B", "forward", "up", "", "E2", 4)
	insert("E5", "C", "apply", "up", "", "", 5)

	labels := func(items []migration.CommittedExecutionSummary) []string {
		names := map[security.ID]string{}
		for label, id := range ids {
			names[id] = label
		}
		result := make([]string, len(items))
		for i, item := range items {
			result[i] = names[item.ID]
		}
		return result
	}
	full, err := f.m2.ListExecutions(ctx, f.db, migration.ExecutionQuery{})
	must(t, err)
	if got := labels(full.Items); len(got) != 5 || got[0] != "E5" || got[4] != "E1" {
		t.Fatalf("full history = %v", got)
	}
	hidden, err := f.m2.ListExecutions(ctx, f.db, migration.ExecutionQuery{HideRollback: true})
	must(t, err)
	if got := labels(hidden.Items); len(got) != 3 || got[0] != "E5" || got[1] != "E4" || got[2] != "E1" {
		t.Fatalf("hide rollback = %v, want [E5 E4 E1]", got)
	}
	paged, err := f.m2.ListExecutions(ctx, f.db, migration.ExecutionQuery{PageQuery: migration.PageQuery{Page: 1, Limit: 2}, HideRollback: true})
	must(t, err)
	if len(paged.Items) != 2 || !paged.HasNext {
		t.Fatalf("hide rollback paging = %+v", paged)
	}

	position, err = f.m2.ReadPosition(ctx, f.db)
	must(t, err)
	if got := labels(position.Active); len(got) != 3 || got[0] != "E1" || got[1] != "E4" || got[2] != "E5" || len(position.Forward) != 0 {
		t.Fatalf("final position = %v forward %d", got, len(position.Forward))
	}

	execution, err := f.m2.FindExecution(ctx, f.db, ids["E3"])
	must(t, err)
	if execution.RevertsExecutionID != ids["E2"] || execution.Direction != migration.DirectionDown || len(execution.Statements) != 2 ||
		execution.Statements[1].Params[0].Reveal() != "x" || *execution.Statements[1].Batch != 3 || execution.Definition.DownSQL.Reveal() != "SELECT 2" {
		t.Fatalf("execution = %+v", execution)
	}
	statements, err := f.m2.ListExecutedStatements(ctx, f.db, ids["E3"], migration.PageQuery{Page: 2, Limit: 1})
	must(t, err)
	if len(statements.Items) != 1 || statements.HasNext || statements.Items[0].Query.Reveal() != "SELECT 2" {
		t.Fatalf("statements page 2 = %+v", statements)
	}
	_, err = f.m2.ListExecutedStatements(ctx, insertDatabase(t, f.pool, "x"), ids["E3"], migration.PageQuery{})
	expectErr(t, err, security.ErrNotFound)
}

func TestM2BuildPositionRollbackOfForward(t *testing.T) {
	up := func(id, forwardOf string) migration.CommittedExecutionSummary {
		operation := migration.ChangeApply
		if forwardOf != "" {
			operation = migration.ChangeForward
		}
		return migration.CommittedExecutionSummary{ID: security.ID(id), Direction: migration.DirectionUp, Operation: operation, ForwardOfExecutionID: security.ID(forwardOf)}
	}
	down := func(id, reverts string) migration.CommittedExecutionSummary {
		return migration.CommittedExecutionSummary{ID: security.ID(id), Direction: migration.DirectionDown, Operation: migration.ChangeRollback, RevertsExecutionID: security.ID(reverts)}
	}
	active, forward, err := buildPosition([]migration.CommittedExecutionSummary{
		up("a", ""), up("b", ""), up("c", ""), down("rc", "c"), down("rb", "b"), up("fb", "b"), down("rfb", "fb"),
	})
	must(t, err)
	if len(active) != 1 || active[0].ID != "a" || len(forward) != 2 || forward[0].ID != "b" || forward[1].ID != "c" {
		t.Fatalf("active=%v forward=%v", active, forward)
	}
	if _, _, err := buildPosition([]migration.CommittedExecutionSummary{down("x", "missing")}); !errors.Is(err, security.ErrUnavailable) {
		t.Fatalf("inconsistent history error = %v", err)
	}
}
