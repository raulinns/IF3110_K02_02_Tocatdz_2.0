package migration

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type WorkflowRepository interface {
	WorkflowStore
	DefinitionStore
	ChangeRequestStore
	JobQueue
	JobLocator
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
	WorkflowUnsupportedChange WorkflowValidation = "Pengajuan ini hanya menerima apply tanpa target rollback atau job pemulihan."
	WorkflowNoGroups          WorkflowValidation = "Database belum memiliki grup pemberi persetujuan."
	WorkflowMissingLeader     WorkflowValidation = "Setiap grup dengan akses database harus memiliki leader aktif."
)

func workflowError(err error) error {
	var validation WorkflowValidation
	if errors.As(err, &validation) {
		switch validation {
		case WorkflowPostgreSQLOnly, WorkflowUnsupportedChange, WorkflowNoGroups, WorkflowMissingLeader:
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
	return s.definitions.CreateDefinition(ctx, req, input)
}

func (s *WorkflowService) UpdateDefinition(ctx context.Context, req Request, id security.ID, revision uint64, input DefinitionInput) (MigrationDefinition, error) {
	if err := s.writeAccess(ctx, req, security.ActionUpdateMigration); err != nil {
		return MigrationDefinition{}, err
	}
	return s.definitions.UpdateDefinition(ctx, req, id, revision, input)
}

func (s *WorkflowService) Submit(ctx context.Context, req Request, input SubmitChange) (ChangeRequest, error) {
	if err := s.writeAccess(ctx, req, security.ActionSubmitMigration); err != nil {
		return ChangeRequest{}, err
	}
	if !validWorkflowRevision(input.MigrationRevision) || !security.ValidID(input.MigrationID) {
		return ChangeRequest{}, security.ErrInvalidInput
	}
	if input.Kind != ChangeApply || input.TargetExecutionID != "" || input.SourceJobID != "" {
		return ChangeRequest{}, WorkflowUnsupportedChange
	}
	draft, err := s.deps.Store.FindDefinition(ctx, req.DatabaseID, input.MigrationID)
	if err != nil {
		return ChangeRequest{}, safeError(err)
	}
	if draft.ID != input.MigrationID || draft.DatabaseID != req.DatabaseID {
		return ChangeRequest{}, security.ErrUnavailable
	}
	if draft.Revision != input.MigrationRevision || draft.State != DefinitionDraft || draft.FirstExecutionAt != nil {
		return ChangeRequest{}, security.ErrConflict
	}
	if draft.Checksum != ChecksumDefinition(draft.DefinitionInput) {
		return ChangeRequest{}, security.ErrUnavailable
	}
	if err := ValidateDefinition(ctx, draft.DefinitionInput); err != nil {
		return ChangeRequest{}, err
	}
	id, err := s.newWorkflowID()
	if err != nil {
		return ChangeRequest{}, err
	}
	at := s.workflowTime()
	change := ChangeRequest{
		ID: id, DatabaseID: req.DatabaseID, SubmitChange: input, Revision: 1, Checksum: draft.Checksum,
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
		if current.State != DefinitionDraft || current.FirstExecutionAt != nil ||
			current.Revision != draft.Revision || current.Checksum != draft.Checksum {
			return security.ErrConflict
		}
		position, err := tx.Position()
		if err != nil {
			return err
		}
		if position.BlockedByJobID != "" || len(position.Forward) != 0 {
			return security.ErrConflict
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
					return err
				}
			}
		}
		return security.ErrForbidden
	})
	if err != nil {
		return ChangeRequest{}, workflowError(err)
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
	spec := JobSpec{ID: jobID, ExecutionID: executionID, DatabaseID: req.DatabaseID, ChangeRequestID: id,
		RequestedBy: req.Principal.UserID, MaxAttempts: DefaultMaxAttempts, RequestID: security.RequestIDFromContext(ctx), QueuedAt: s.workflowTime()}
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
	if err != nil {
		return JobView{}, workflowError(err)
	}
	return result, nil
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
