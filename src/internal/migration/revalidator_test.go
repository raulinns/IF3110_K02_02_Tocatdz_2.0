package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func rvID(n int) security.ID { return security.ID(fmt.Sprintf("10000000-0000-4000-8000-%012d", n)) }

type snapshotReader struct {
	snapshot RevalidationSnapshot
	err      error
}

func (s snapshotReader) ReadRevalidation(_ context.Context, db, job security.ID) (RevalidationSnapshot, error) {
	if db != s.snapshot.Job.DatabaseID || job != s.snapshot.Job.ID {
		return RevalidationSnapshot{}, security.ErrNotFound
	}
	return s.snapshot, s.err
}

func runnableSnapshot(input DefinitionInput) RevalidationSnapshot {
	db, user, def, cr, job, execution := rvID(1), rvID(2), rvID(3), rvID(4), rvID(5), rvID(6)
	definition := MigrationDefinition{ID: def, DatabaseID: db, State: DefinitionFrozen, Revision: 2, Checksum: ChecksumDefinition(input), DefinitionInput: input}
	required := RequiredApproval{GroupID: rvID(7), LeaderUserID: rvID(8), Access: security.AccessViewOnly}
	decision := ApprovalDecision{GroupID: required.GroupID, ActorID: required.LeaderUserID, Decision: DecisionApprove, DecidedAt: time.Unix(20, 0)}
	return RevalidationSnapshot{
		Job:           JobView{JobSpec: JobSpec{ID: job, DatabaseID: db, ChangeRequestID: cr, ExecutionID: execution, RequestedBy: user}, MigrationID: def, Kind: ChangeApply, Mode: input.Mode, Status: JobRunning, Attempt: 1},
		ChangeRequest: ChangeRequest{ID: cr, DatabaseID: db, SubmitChange: SubmitChange{MigrationID: def, MigrationRevision: 2, Kind: ChangeApply}, Status: CRQueued, RequestedBy: user, Checksum: definition.Checksum, Approvals: []GroupApproval{{RequiredApproval: required, Decision: &decision}}, ExpectedPosition: 4},
		Definition:    definition, Position: Position{Version: 4}, Requester: security.Account{ID: user, Active: true, AuthVersion: 2}, RequesterAccess: security.AccessViewEdit, CurrentApprovers: []RequiredApproval{required},
	}
}

func revalidateFixture(t *testing.T, s RevalidationSnapshot, claimed JobView) (ExecutionPlan, error) {
	t.Helper()
	r, err := NewRevalidator(snapshotReader{snapshot: s}, security.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	return r.Revalidate(context.Background(), claimed)
}

func TestRevalidatorBuildsPlanOnlyForCurrentAuthorizedClaim(t *testing.T) {
	s := runnableSnapshot(normalDefinition())
	plan, err := revalidateFixture(t, s, s.Job)
	if err != nil || plan.Direction != DirectionUp || plan.ReceiptExecutionID != s.Job.ExecutionID || plan.ActorID != s.ChangeRequest.RequestedBy || plan.Definition.Checksum != s.Definition.Checksum {
		t.Fatal("incorrect execution plan", err)
	}
	for _, test := range []struct {
		name string
		edit func(*RevalidationSnapshot)
	}{
		{"job re-claimed", func(s *RevalidationSnapshot) { s.Job.Attempt++ }},
		{"not queued CR", func(s *RevalidationSnapshot) { s.ChangeRequest.Status = CRApproved }},
		{"foreign CR", func(s *RevalidationSnapshot) { s.ChangeRequest.DatabaseID = rvID(10) }},
		{"draft", func(s *RevalidationSnapshot) { s.Definition.State = DefinitionDraft }},
		{"revision changed", func(s *RevalidationSnapshot) { s.Definition.Revision++ }},
		{"checksum changed", func(s *RevalidationSnapshot) { s.ChangeRequest.Checksum[0] ^= 1 }},
		{"definition bytes changed without checksum", func(s *RevalidationSnapshot) { s.Definition.DownSQL = security.NewSecret("SELECT 99") }},
		{"requester disabled", func(s *RevalidationSnapshot) { s.Requester.Active = false }},
		{"viewer requester", func(s *RevalidationSnapshot) { s.RequesterAccess = security.AccessViewOnly }},
		{"leader changed", func(s *RevalidationSnapshot) { s.CurrentApprovers[0].LeaderUserID = rvID(20) }},
		{"grant changed", func(s *RevalidationSnapshot) { s.CurrentApprovers[0].Access = security.AccessViewEdit }},
		{"group added", func(s *RevalidationSnapshot) {
			s.CurrentApprovers = append(s.CurrentApprovers, RequiredApproval{GroupID: rvID(19), LeaderUserID: rvID(20), Access: security.AccessViewOnly})
		}},
		{"group removed", func(s *RevalidationSnapshot) { s.CurrentApprovers = nil }},
		{"approval missing", func(s *RevalidationSnapshot) { s.ChangeRequest.Approvals[0].Decision = nil }},
		{"approval rejected", func(s *RevalidationSnapshot) { s.ChangeRequest.Approvals[0].Decision.Decision = DecisionReject }},
		{"approval by wrong user", func(s *RevalidationSnapshot) { s.ChangeRequest.Approvals[0].Decision.ActorID = rvID(20) }},
		{"self approval", func(s *RevalidationSnapshot) {
			s.CurrentApprovers[0].LeaderUserID = s.Requester.ID
			s.ChangeRequest.Approvals[0].LeaderUserID = s.Requester.ID
			s.ChangeRequest.Approvals[0].Decision.ActorID = s.Requester.ID
		}},
		{"position changed", func(s *RevalidationSnapshot) { s.Position.Version++ }},
		{"blocked", func(s *RevalidationSnapshot) { s.Position.BlockedByJobID = rvID(20) }},
		{"redo pending", func(s *RevalidationSnapshot) { s.Position.Forward = []CommittedExecutionSummary{{ID: rvID(20)}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := runnableSnapshot(normalDefinition())
			claimed := s.Job
			test.edit(&s)
			plan, err := revalidateFixture(t, s, claimed)
			if !errors.Is(err, security.ErrConflict) || plan.JobID != "" {
				t.Fatalf("unsafe plan or wrong error: %v", err)
			}
		})
	}
}

func TestRevalidatorSelectsOriginalHistoryDefinitionAndLinearTarget(t *testing.T) {
	for _, kind := range []ChangeKind{ChangeRollback, ChangeForward} {
		s := runnableSnapshot(normalDefinition())
		s.Job.Kind = kind
		s.ChangeRequest.Kind = kind
		d := s.Definition
		execution := Execution{ID: rvID(15), DatabaseID: d.DatabaseID, MigrationID: d.ID, MigrationRevision: d.Revision, Checksum: d.Checksum, Direction: DirectionUp, Definition: d.DefinitionInput}
		s.TargetExecution = &execution
		s.ChangeRequest.TargetExecutionID = execution.ID
		summary := CommittedExecutionSummary{ID: execution.ID, DatabaseID: d.DatabaseID, MigrationID: d.ID, Checksum: d.Checksum, MigrationRevision: d.Revision, Direction: DirectionUp}
		want := DirectionUp
		if kind == ChangeRollback {
			s.Position.Active = []CommittedExecutionSummary{summary}
			want = DirectionDown
		} else {
			s.Position.Forward = []CommittedExecutionSummary{summary}
		}
		plan, err := revalidateFixture(t, s, s.Job)
		if err != nil || plan.Direction != want || plan.TargetExecutionID != execution.ID {
			t.Fatalf("%s: %v", kind, err)
		}
		if kind == ChangeRollback {
			s.Position.Active = append(s.Position.Active, CommittedExecutionSummary{ID: rvID(19)})
		} else {
			s.Position.Forward = append([]CommittedExecutionSummary{{ID: rvID(19)}}, summary)
		}
		if _, err := revalidateFixture(t, s, s.Job); !errors.Is(err, security.ErrConflict) {
			t.Fatalf("%s bypassed history order", kind)
		}
	}
}

func TestRevalidatorRecoveryPreservesLineageAndReceipt(t *testing.T) {
	for _, kind := range []ChangeKind{ChangeResume, ChangeRevertBatches} {
		s := runnableSnapshot(batchDefinition())
		s.Job.Kind = kind
		s.ChangeRequest.Kind = kind
		source := s.Job
		source.ID = rvID(12)
		source.ChangeRequestID = rvID(13)
		source.ExecutionID = rvID(14)
		source.Kind = ChangeApply
		source.Status = JobPartial
		scope := "-2"
		source.ScopeMax = &scope
		s.SourceJob = &source
		s.ChangeRequest.SourceJobID = source.ID
		s.Position.BlockedByJobID = source.ID
		s.SourcePlan = &ExecutionPlan{JobID: source.ID, ChangeRequestID: source.ChangeRequestID, DatabaseID: source.DatabaseID, MigrationID: source.MigrationID, ReceiptExecutionID: source.ExecutionID, Kind: source.Kind, Direction: DirectionUp, Definition: s.Definition}
		want := DirectionDown
		if kind == ChangeResume {
			s.Job.ExecutionID = source.ExecutionID
			want = DirectionUp
		}
		plan, err := revalidateFixture(t, s, s.Job)
		if err != nil || plan.Direction != want || plan.SourceExecutionID != source.ExecutionID || plan.ScopeMax == nil || *plan.ScopeMax != scope {
			t.Fatalf("%s: %v", kind, err)
		}
		if kind == ChangeResume {
			s.Job.ExecutionID = rvID(16)
		} else {
			s.Job.ExecutionID = source.ExecutionID
		}
		if _, err := revalidateFixture(t, s, s.Job); !errors.Is(err, security.ErrConflict) {
			t.Fatalf("%s used wrong receipt", kind)
		}
	}
}

func TestRevalidatorDoesNotExposeReaderFailure(t *testing.T) {
	s := runnableSnapshot(normalDefinition())
	r, err := NewRevalidator(snapshotReader{snapshot: s, err: errors.New("private driver DSN")}, security.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := r.Revalidate(context.Background(), s.Job)
	if !errors.Is(err, security.ErrUnavailable) || strings.Contains(err.Error(), "DSN") || plan.JobID != "" {
		t.Fatal("unsafe reader failure", err)
	}
}
