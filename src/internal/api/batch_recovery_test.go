package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type batchRecoveryFixture struct {
	*historyWorkflowFixture
	source   migration.JobView
	original migration.ExecutionPlan
}

func (f *batchRecoveryFixture) WithWorkflow(_ context.Context, _ migration.Request, fn func(migration.WorkflowTx) error) error {
	return fn(f)
}

func (f *batchRecoveryFixture) RecoverySource(id security.ID) (migration.JobView, *migration.ExecutionPlan, error) {
	if id != f.source.ID {
		return migration.JobView{}, nil, security.ErrNotFound
	}
	return f.source, &f.original, nil
}

func TestBatchRecoveryRequiresApprovalAndRetainsReceipt(t *testing.T) {
	for _, kind := range []migration.ChangeKind{migration.ChangeResume, migration.ChangeRevertBatches} {
		t.Run(string(kind), func(t *testing.T) {
			editor := newFixture(t)
			editor.store.access = security.AccessViewEdit
			leader := newFixture(t)
			leader.store.account.Account.ID = adminTestMember
			f := &batchRecoveryFixture{historyWorkflowFixture: &historyWorkflowFixture{workflowFixture: &workflowFixture{}}}
			input := migration.DefinitionInput{Title: "Batch", UpSQL: security.NewSecret("value = 1"), DownSQL: security.NewSecret("value = 0"),
				Mode: migration.ModeBatch, Batch: &migration.BatchConfig{Schema: "public", Table: "widgets", KeyColumn: "id", BatchSize: 100}}
			f.definition = migration.MigrationDefinition{ID: historyID(30), DatabaseID: testDatabase, State: migration.DefinitionFrozen,
				Revision: 1, DefinitionInput: input, Checksum: migration.ChecksumDefinition(input)}
			f.source = migration.JobView{JobSpec: migration.JobSpec{ID: historyID(31), DatabaseID: testDatabase,
				ExecutionID: historyID(32), ChangeRequestID: historyID(33)}, MigrationID: f.definition.ID,
				Mode: migration.ModeBatch, Kind: migration.ChangeApply, Status: migration.JobPartial}
			f.original = migration.ExecutionPlan{JobID: f.source.ID, DatabaseID: testDatabase, MigrationID: f.definition.ID,
				ChangeRequestID: f.source.ChangeRequestID, ReceiptExecutionID: f.source.ExecutionID, Kind: migration.ChangeApply,
				Direction: migration.DirectionUp, Definition: f.definition}
			f.position = migration.Position{Version: 4, BlockedByJobID: f.source.ID}
			f.groups = []migration.RequiredApproval{{GroupID: adminTestGroup, LeaderUserID: adminTestMember, Access: security.AccessViewOnly}}
			send := newWorkflowTestClient(t, editor, f, f)
			review := newWorkflowTestClient(t, leader, f, f)
			path := Prefix + "/databases/" + string(testDatabase) + "/change-requests"
			body := fmt.Sprintf(`{"migration_id":"%s","migration_revision":1,"kind":"%s","source_job_id":"%s"}`, f.definition.ID, kind, f.source.ID)
			review("POST", path, body, 403)
			f.source.Status = migration.JobUncertain
			send("POST", path, body, 409)
			f.source.Status = migration.JobPartial
			f.position.BlockedByJobID = historyID(99)
			send("POST", path, body, 409)
			f.position.BlockedByJobID = f.source.ID
			f.original.Definition.Checksum[0] ^= 1
			send("POST", path, body, 409)
			f.original.Definition.Checksum = f.definition.Checksum
			location := send("POST", path, body, 201).Header().Get("Location")
			send("POST", location+"/execute", `{"revision":1}`, 409)
			vote := fmt.Sprintf(`{"group_id":"%s","revision":1,"decision":"approve"}`, adminTestGroup)
			review("POST", location+"/approvals", vote, 200)
			f.source.Status = migration.JobSucceeded
			send("POST", location+"/execute", `{"revision":2}`, 409)
			f.source.Status = migration.JobPartial
			queued := send("POST", location+"/execute", `{"revision":2}`, 202)
			if kind == migration.ChangeResume && f.job.ExecutionID != f.source.ExecutionID {
				t.Fatal("resume allocated a different receipt")
			}
			if kind == migration.ChangeRevertBatches && f.job.ExecutionID == f.source.ExecutionID {
				t.Fatal("compensation reused the original UP receipt")
			}
			replayed := send("POST", location+"/execute", `{"revision":2}`, 202)
			if replayed.Header().Get("Location") != queued.Header().Get("Location") || !strings.Contains(replayed.Body.String(), string(f.job.ID)) {
				t.Fatal("recovery enqueue replay created another job")
			}
			// A failed compensation can be continued with either recovery action.
			f.source.Status = migration.JobPartiallyReverted
			f.source.Kind = migration.ChangeRevertBatches
			f.original.Kind = f.source.Kind
			f.original.Direction = migration.DirectionDown
			f.original.SourceExecutionID = historyID(90)
			send("POST", path, body, 201)
			f.definition.Irreversible = true
			f.definition.DownSQL = security.Secret{}
			f.definition.IrreversibleReason = "Cannot restore"
			f.definition.Checksum = migration.ChecksumDefinition(f.definition.DefinitionInput)
			f.original.Definition = f.definition
			send("POST", path, body, 409)
		})
	}
}
