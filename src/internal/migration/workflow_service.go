package migration

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type WorkflowRepository interface {
	WorkflowStore
	DefinitionStore
	ChangeRequestStore
	JobQueue
	JobLocator
	ExecutionStore
}

type WorkflowEngine interface {
	Engine(context.Context, security.ID) (dbms.Engine, error)
}

type WorkflowConfig struct {
	Store      WorkflowRepository
	Authorizer security.Authorizer
	Engines    WorkflowEngine
	IDs        IDs
	Clock      security.Clock
}

type WorkflowService struct {
	deps        WorkflowConfig
	definitions *DefinitionService
}

func NewWorkflowService(cfg WorkflowConfig) (*WorkflowService, error) {
	definitions, err := NewDefinitionService(cfg.Store, cfg.Authorizer, cfg.IDs, cfg.Clock)
	if err != nil || missing(cfg.Engines) {
		return nil, security.ErrInvalidInput
	}
	return &WorkflowService{deps: cfg, definitions: definitions}, nil
}

type WorkflowValidation string

func (e WorkflowValidation) Error() string { return string(e) }
func (WorkflowValidation) Unwrap() error   { return security.ErrInvalidInput }

const (
	WorkflowPostgreSQLOnly    WorkflowValidation = "Workflow migration hanya tersedia untuk PostgreSQL."
	WorkflowUnsupportedChange WorkflowValidation = "Jenis perubahan atau referensi target tidak valid untuk operasi ini."
	WorkflowIrreversible      WorkflowValidation = "Eksekusi ini tidak dapat di-rollback karena tidak memiliki SQL down."
	WorkflowNoGroups          WorkflowValidation = "Database belum memiliki grup pemberi persetujuan."
	WorkflowMissingLeader     WorkflowValidation = "Setiap grup dengan akses database harus memiliki leader aktif."
)

func workflowError(err error) error {
	var validation WorkflowValidation
	if errors.As(err, &validation) {
		switch validation {
		case WorkflowPostgreSQLOnly, WorkflowUnsupportedChange, WorkflowNoGroups, WorkflowMissingLeader, WorkflowIrreversible:
			return validation
		}
	}
	return safeError(err)
}

func validWorkflowRevision(revision uint64) bool { return revision > 0 && revision <= (1<<53)-1 }

func (s *WorkflowService) writeAccess(ctx context.Context, req Request, action security.DatabaseAction) error {
	if err := s.access(ctx, req, action); err != nil {
		return err
	}
	engine, err := s.deps.Engines.Engine(ctx, req.DatabaseID)
	if err != nil {
		return safeError(err)
	}
	if engine != dbms.PostgreSQL {
		return WorkflowPostgreSQLOnly
	}
	return nil
}

func (s *WorkflowService) access(ctx context.Context, req Request, action security.DatabaseAction) error {
	return safeError(s.deps.Authorizer.RequireDatabase(ctx, req.Principal, req.DatabaseID, action))
}

func (s *WorkflowService) CreateDefinition(ctx context.Context, req Request, input DefinitionInput) (MigrationDefinition, error) {
	if err := s.writeAccess(ctx, req, security.ActionCreateMigration); err != nil {
		return MigrationDefinition{}, err
	}
	result, err := s.definitions.CreateDefinition(ctx, req, input)
	if err == nil {
		workflowLog(ctx, req, "migration.created", "succeeded", result.ID, "", result.Revision)
	}
	return result, err
}

func (s *WorkflowService) UpdateDefinition(ctx context.Context, req Request, id security.ID, revision uint64, input DefinitionInput) (MigrationDefinition, error) {
	if err := s.writeAccess(ctx, req, security.ActionUpdateMigration); err != nil {
		return MigrationDefinition{}, err
	}
	result, err := s.definitions.UpdateDefinition(ctx, req, id, revision, input)
	if err == nil {
		workflowLog(ctx, req, "migration.updated", "succeeded", result.ID, "", result.Revision)
	}
	return result, err
}

func (s *WorkflowService) Submit(ctx context.Context, req Request, input SubmitChange) (ChangeRequest, error) {
	action := security.ActionSubmitMigration
	switch input.Kind {
	case ChangeRollback:
		action = security.ActionRollbackMigration
	case ChangeForward:
		action = security.ActionForwardMigration
	case ChangeResume:
		action = security.ActionResumeMigration
	case ChangeRevertBatches:
		action = security.ActionRevertBatches
	}
	if err := s.writeAccess(ctx, req, action); err != nil {
		return ChangeRequest{}, err
	}
	if !validWorkflowRevision(input.MigrationRevision) || !security.ValidID(input.MigrationID) {
		return ChangeRequest{}, security.ErrInvalidInput
	}
	if input.SourceJobID != "" && input.Kind != ChangeApply && input.Kind != ChangeResume && input.Kind != ChangeRevertBatches {
		return ChangeRequest{}, WorkflowUnsupportedChange
	}
	switch input.Kind {
	case ChangeApply:
		if input.TargetExecutionID != "" || (input.SourceJobID != "" && !security.ValidID(input.SourceJobID)) {
			return ChangeRequest{}, WorkflowUnsupportedChange
		}
	case ChangeRollback, ChangeForward:
		if !security.ValidID(input.TargetExecutionID) {
			return ChangeRequest{}, WorkflowUnsupportedChange
		}
	case ChangeResume, ChangeRevertBatches:
		if !security.ValidID(input.SourceJobID) || input.TargetExecutionID != "" {
			return ChangeRequest{}, WorkflowUnsupportedChange
		}
	default:
		return ChangeRequest{}, WorkflowUnsupportedChange
	}
	definition, err := s.deps.Store.FindDefinition(ctx, req.DatabaseID, input.MigrationID)
	if err != nil {
		return ChangeRequest{}, safeError(err)
	}
	if definition.ID != input.MigrationID || definition.DatabaseID != req.DatabaseID {
		return ChangeRequest{}, security.ErrUnavailable
	}
	if definition.Revision != input.MigrationRevision || !validChangeDefinition(input, definition) {
		return ChangeRequest{}, security.ErrConflict
	}
	if definition.Checksum != ChecksumDefinition(definition.DefinitionInput) {
		return ChangeRequest{}, security.ErrUnavailable
	}
	if err := ValidateDefinition(ctx, definition.DefinitionInput); err != nil {
		return ChangeRequest{}, err
	}
	id, err := s.newWorkflowID()
	if err != nil {
		return ChangeRequest{}, err
	}
	at := s.workflowTime()
	change := ChangeRequest{
		ID: id, DatabaseID: req.DatabaseID, SubmitChange: input, Revision: 1, Checksum: definition.Checksum,
		Status: CRPendingReview, RequestedBy: req.Principal.UserID, CreatedAt: at, UpdatedAt: at,
	}
	err = s.deps.Store.WithWorkflow(ctx, req, func(tx WorkflowTx) error {
		if err := tx.RequireAccess(security.AccessViewEdit); err != nil {
			return err
		}
		current, err := tx.Definition(input.MigrationID)
		if err != nil {
			return err
		}
		if !validChangeDefinition(input, current) ||
			current.Revision != definition.Revision || current.Checksum != definition.Checksum {
			return security.ErrConflict
		}
		position, err := tx.Position()
		if err != nil {
			return err
		}
		if err := checkSubmissionPosition(tx, position, change, current); err != nil {
			return err
		}
		required, err := tx.RequiredApprovers()
		if err != nil {
			return err
		}
		if len(required) == 0 {
			return WorkflowNoGroups
		}
		change.Approvals = make([]GroupApproval, 0, len(required))
		for _, group := range required {
			if !security.ValidID(group.LeaderUserID) {
				return WorkflowMissingLeader
			}
			change.Approvals = append(change.Approvals, GroupApproval{RequiredApproval: group})
		}
		change.ExpectedPosition = position.Version
		return tx.CreateChangeRequest(change)
	})
	if err != nil {
		return ChangeRequest{}, workflowError(err)
	}
	workflowLog(ctx, req, "migration.submitted", "succeeded", change.MigrationID, change.ID, change.MigrationRevision)
	return change, nil
}

func (s *WorkflowService) Decide(ctx context.Context, req Request, id security.ID, revision uint64, groupID security.ID, decision Decision, note string) (ChangeRequest, error) {
	if err := s.writeAccess(ctx, req, security.ActionViewMigration); err != nil {
		return ChangeRequest{}, err
	}
	if !validWorkflowRevision(revision) || !security.ValidID(id) || !security.ValidID(groupID) ||
		(decision != DecisionApprove && decision != DecisionReject) || !utf8.ValidString(note) ||
		utf8.RuneCountInString(note) > MaxApprovalNote || strings.ContainsRune(note, 0) {
		return ChangeRequest{}, security.ErrInvalidInput
	}
	vote := ApprovalDecision{GroupID: groupID, ActorID: req.Principal.UserID, Decision: decision, Note: note, DecidedAt: s.workflowTime()}
	var result ChangeRequest
	newDecision := false
	err := s.deps.Store.WithWorkflow(ctx, req, func(tx WorkflowTx) error {
		if err := tx.RequireAccess(security.AccessViewOnly); err != nil {
			return err
		}
		change, err := tx.ChangeRequest(id)
		if err != nil {
			return err
		}
		if change.RequestedBy == vote.ActorID {
			return security.ErrForbidden
		}
		required, err := tx.RequiredApprovers()
		if err != nil {
			return err
		}
		// Check the current group even when the store sees an exact replay.
		for _, snapshot := range change.Approvals {
			if snapshot.GroupID != groupID || snapshot.LeaderUserID != vote.ActorID {
				continue
			}
			for _, group := range required {
				if group.GroupID == groupID && group.LeaderUserID == vote.ActorID && group.Access == snapshot.Access {
					result, err = tx.RecordDecision(id, revision, vote)
					newDecision = err == nil && snapshot.Decision == nil
					return err
				}
			}
		}
		return security.ErrForbidden
	})
	if err != nil {
		return ChangeRequest{}, workflowError(err)
	}
	if newDecision {
		event, status := "migration.approved", "approved"
		if decision == DecisionReject {
			event, status = "migration.rejected", "rejected"
		}
		workflowLog(ctx, req, event, status, result.MigrationID, id, result.MigrationRevision, slog.String("group_id", string(groupID)))
	}
	return result, nil
}

func (s *WorkflowService) Enqueue(ctx context.Context, req Request, id security.ID, revision uint64) (JobView, error) {
	if err := s.writeAccess(ctx, req, security.ActionExecuteMigration); err != nil {
		return JobView{}, err
	}
	if !validWorkflowRevision(revision) || !security.ValidID(id) {
		return JobView{}, security.ErrInvalidInput
	}
	jobID, err := s.newWorkflowID()
	if err != nil {
		return JobView{}, err
	}
	executionID, err := s.newWorkflowID()
	if err != nil {
		return JobView{}, err
	}
	ctx, end := tracing.Start(ctx, "job.enqueue", tracing.Producer)
	spec := JobSpec{ID: jobID, ExecutionID: executionID, DatabaseID: req.DatabaseID, ChangeRequestID: id,
		RequestedBy: req.Principal.UserID, MaxAttempts: DefaultMaxAttempts, RequestID: security.RequestIDFromContext(ctx),
		TraceContext: security.NewSecret(tracing.Inject(ctx)), QueuedAt: s.workflowTime()}
	var result JobView
	err = s.deps.Store.WithWorkflow(ctx, req, func(tx WorkflowTx) error {
		if err := tx.RequireAccess(security.AccessViewEdit); err != nil {
			return err
		}
		change, err := tx.ChangeRequest(id)
		if err != nil {
			return err
		}
		switch change.Status {
		case CRApproved:
			if change.Revision != revision {
				return security.ErrConflict
			}
			required, err := tx.RequiredApprovers()
			if err != nil {
				return err
			}
			if !approvalsStillValid(change, required) {
				return security.ErrConflict
			}
			definition, err := tx.Definition(change.MigrationID)
			if err != nil {
				return err
			}
			position, err := tx.Position()
			if err != nil {
				return err
			}
			if position.Version != change.ExpectedPosition {
				return security.ErrConflict
			}
			if err := checkSubmissionPosition(tx, position, change, definition); err != nil {
				return err
			}
			if change.Kind == ChangeResume {
				source, _, err := recoverySource(tx, change.SourceJobID)
				if err != nil {
					return err
				}
				spec.ExecutionID = source.ExecutionID
			}
		case CRQueued, CRRunning, CRSucceeded, CRFailed, CRPartial, CRUncertain, CRPartiallyReverted:
			// A CR starts at revision one and advances once per persisted vote
			// before enqueue. Subsequent worker transitions also advance it.
			if revision != uint64(len(change.Approvals))+1 {
				return security.ErrConflict
			}
		default:
			return security.ErrConflict
		}
		result, err = tx.EnqueueApproved(id, revision, spec)
		return err
	})
	if err == nil && result.ID == jobID {
		workflowLog(ctx, req, "job.enqueued", "queued", result.MigrationID, id, 0,
			slog.String("job_id", string(result.ID)), slog.String("execution_id", string(result.ExecutionID)))
	}
	end(err != nil)
	if err != nil {
		return JobView{}, workflowError(err)
	}
	return result, nil
}

func workflowLog(ctx context.Context, req Request, event, status string, migrationID, changeID security.ID, revision uint64, extra ...slog.Attr) {
	if logger := logging.FromContext(ctx); logger != nil {
		op := logging.Operation{Actor: string(req.Principal.UserID), DatabaseID: req.DatabaseID, MigrationID: migrationID,
			ChangeRequestID: changeID, Revision: revision}
		attrs := append(op.Attrs(), slog.String("event", event), slog.String("status", status))
		logger.LogAttrs(ctx, slog.LevelInfo, event, append(attrs, extra...)...)
	}
}

func (s *WorkflowService) newWorkflowID() (security.ID, error) {
	id, err := s.deps.IDs.GenerateID()
	if err != nil || !security.ValidID(id) {
		return "", security.ErrUnavailable
	}
	return id, nil
}

func (s *WorkflowService) workflowTime() time.Time {
	return s.deps.Clock.Now().UTC().Truncate(time.Microsecond)
}
