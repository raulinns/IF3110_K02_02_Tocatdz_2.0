package migration

import (
	"context"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"strconv"
)

type domainRevalidator struct{ reader RevalidationReader }

// NewRevalidator has the signature of the integrator's RevalidatorFactory.
// Lease time and fencing belong to the worker store, not to the domain clock.
func NewRevalidator(reader RevalidationReader, clock security.Clock) (Revalidator, error) {
	if missing(reader) || missing(clock) {
		return nil, security.ErrInvalidInput
	}
	return &domainRevalidator{reader: reader}, nil
}

// RevalidationRejection is a fixed, safe diagnostic for worker logs.
type RevalidationRejection string

func (r RevalidationRejection) Error() string { return "migration revalidation failed: " + string(r) }
func (r RevalidationRejection) Unwrap() error { return security.ErrConflict }

func (r *domainRevalidator) Revalidate(ctx context.Context, claimed JobView) (ExecutionPlan, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionPlan{}, err
	}
	if claimed.Status != JobRunning || claimed.Attempt < 1 || !security.ValidID(claimed.DatabaseID) || !security.ValidID(claimed.ID) {
		return ExecutionPlan{}, RevalidationRejection("invalid claim")
	}
	snapshot, err := r.reader.ReadRevalidation(ctx, claimed.DatabaseID, claimed.ID)
	if err != nil {
		return ExecutionPlan{}, safeError(err)
	}
	if err := ctx.Err(); err != nil {
		return ExecutionPlan{}, err
	}
	plan, err := buildRevalidatedPlan(snapshot, claimed)
	if err != nil {
		return ExecutionPlan{}, err
	}
	// The approved bytes are also checked against the current safe SQL policy.
	if err := ValidateDefinition(ctx, plan.Definition.DefinitionInput); err != nil {
		if ctx.Err() != nil {
			return ExecutionPlan{}, ctx.Err()
		}
		return ExecutionPlan{}, RevalidationRejection("definition violates SQL policy")
	}
	if err := ctx.Err(); err != nil {
		return ExecutionPlan{}, err
	}
	return plan, nil
}

func buildRevalidatedPlan(s RevalidationSnapshot, claimed JobView) (ExecutionPlan, error) {
	job, change, definition := s.Job, s.ChangeRequest, s.Definition
	if job.ID != claimed.ID || job.DatabaseID != claimed.DatabaseID || job.ChangeRequestID != claimed.ChangeRequestID ||
		job.ExecutionID != claimed.ExecutionID || job.MigrationID != claimed.MigrationID || job.Kind != claimed.Kind ||
		job.Mode != claimed.Mode || job.RequestedBy != claimed.RequestedBy || job.Status != JobRunning || job.Attempt != claimed.Attempt ||
		!security.ValidID(job.ExecutionID) || !security.ValidID(change.ID) || !security.ValidID(definition.ID) ||
		change.Status != CRQueued || change.ID != job.ChangeRequestID || change.Kind != job.Kind ||
		change.DatabaseID != job.DatabaseID || definition.DatabaseID != job.DatabaseID ||
		change.MigrationID != definition.ID || job.MigrationID != definition.ID || job.Mode != definition.Mode {
		return ExecutionPlan{}, RevalidationRejection("job or change request changed")
	}
	if err := CheckRevalidationAuthorization(s); err != nil {
		return ExecutionPlan{}, err
	}
	if definition.State != DefinitionFrozen || definition.Revision == 0 || definition.Revision != change.MigrationRevision ||
		definition.Checksum != change.Checksum || validateDefinitionShape(definition.DefinitionInput) != nil ||
		ChecksumDefinition(definition.DefinitionInput) != change.Checksum {
		return ExecutionPlan{}, RevalidationRejection("approved definition changed")
	}
	if s.Position.Version != change.ExpectedPosition || s.Position.BlockedByJobID != change.SourceJobID {
		return ExecutionPlan{}, RevalidationRejection("database position or recovery barrier changed")
	}
	plan := ExecutionPlan{JobID: job.ID, ChangeRequestID: change.ID, DatabaseID: job.DatabaseID, MigrationID: definition.ID,
		ReceiptExecutionID: job.ExecutionID, ActorID: change.RequestedBy, Kind: change.Kind, Direction: DirectionUp,
		Definition: definition, ExpectedPosition: change.ExpectedPosition, TargetExecutionID: change.TargetExecutionID,
		SourceJobID: change.SourceJobID, ScopeMax: job.ScopeMax}
	switch change.Kind {
	case ChangeApply:
		if change.TargetExecutionID != "" || (change.SourceJobID == "" && len(s.Position.Forward) != 0) {
			return ExecutionPlan{}, RevalidationRejection("apply would break linear history")
		}
		if change.SourceJobID != "" {
			if !validRecoverySource(s) || s.SourceJob.ExecutionID == job.ExecutionID || s.SourceJob.MigrationID == definition.ID {
				return ExecutionPlan{}, RevalidationRejection("invalid repair source")
			}
		}
	case ChangeRollback, ChangeForward:
		if !applyHistoryTarget(s, &plan) {
			return ExecutionPlan{}, RevalidationRejection("invalid history target")
		}
	case ChangeResume, ChangeRevertBatches:
		if !applyRecoverySource(s, &plan) {
			return ExecutionPlan{}, RevalidationRejection("invalid recovery lineage")
		}
	default:
		return ExecutionPlan{}, RevalidationRejection("unknown change kind")
	}
	if plan.ScopeMax != nil {
		value, err := strconv.ParseInt(*plan.ScopeMax, 10, 64)
		if err != nil || strconv.FormatInt(value, 10) != *plan.ScopeMax || plan.Definition.Mode != ModeBatch {
			return ExecutionPlan{}, RevalidationRejection("invalid persisted batch scope")
		}
	}
	return plan, nil
}

// CheckRevalidationAuthorization can also be called by StartRunning with fresh
// account, grant and approval data read under its workflow transaction lock.
func CheckRevalidationAuthorization(s RevalidationSnapshot) error {
	change := s.ChangeRequest
	if !security.ValidID(change.RequestedBy) || s.Requester.ID != change.RequestedBy || !s.Requester.Active ||
		s.Requester.AuthVersion == 0 || !security.Allows(s.RequesterAccess, security.ActionExecuteMigration) {
		return RevalidationRejection("requester lacks current permission")
	}
	if !approvalsStillValid(change, s.CurrentApprovers) {
		return RevalidationRejection("required approvals changed")
	}
	return nil
}

func approvalsStillValid(change ChangeRequest, current []RequiredApproval) bool {
	if len(current) == 0 || len(current) != len(change.Approvals) {
		return false
	}
	groups := make(map[security.ID]RequiredApproval, len(current))
	for _, required := range current {
		if !security.ValidID(required.GroupID) || !security.ValidID(required.LeaderUserID) ||
			!security.Allows(required.Access, security.ActionViewMigration) {
			return false
		}
		if _, exists := groups[required.GroupID]; exists {
			return false
		}
		groups[required.GroupID] = required
	}
	for _, approval := range change.Approvals {
		required, ok := groups[approval.GroupID]
		decision := approval.Decision
		if !ok || required.LeaderUserID != approval.LeaderUserID || required.Access != approval.Access ||
			decision == nil || decision.GroupID != approval.GroupID || decision.ActorID != required.LeaderUserID ||
			decision.ActorID == change.RequestedBy || decision.Decision != DecisionApprove || decision.DecidedAt.IsZero() {
			return false
		}
		delete(groups, approval.GroupID)
	}
	return len(groups) == 0
}

func applyHistoryTarget(s RevalidationSnapshot, plan *ExecutionPlan) bool {
	target := s.TargetExecution
	if s.ChangeRequest.SourceJobID != "" || target == nil || target.ID != plan.TargetExecutionID ||
		target.ID == plan.ReceiptExecutionID {
		return false
	}
	if checkHistoryTarget(historyTarget(s.Position, plan.Kind), *target, plan.Definition, plan.Kind) != nil {
		return false
	}
	if plan.Kind == ChangeRollback {
		plan.Direction = DirectionDown
	}
	plan.Definition.DefinitionInput = target.Definition
	return true
}

func validRecoverySource(s RevalidationSnapshot) bool {
	source := s.SourceJob
	return source != nil && security.ValidID(source.ID) && source.ID != s.Job.ID && source.ID == s.ChangeRequest.SourceJobID &&
		source.DatabaseID == s.Job.DatabaseID && security.ValidID(source.ExecutionID) && source.Mode == ModeBatch &&
		(source.Status == JobPartial || source.Status == JobPartiallyReverted)
}

func applyRecoverySource(s RevalidationSnapshot, plan *ExecutionPlan) bool {
	source, original := s.SourceJob, s.SourcePlan
	if !validRecoverySource(s) || original == nil || plan.TargetExecutionID != "" ||
		plan.Definition.Mode != ModeBatch || source.MigrationID != plan.MigrationID || original.JobID != source.ID ||
		original.ChangeRequestID != source.ChangeRequestID || original.DatabaseID != plan.DatabaseID || original.MigrationID != plan.MigrationID ||
		original.ReceiptExecutionID != source.ExecutionID || original.Kind != source.Kind ||
		original.Definition.Revision != plan.Definition.Revision || original.Definition.Checksum != plan.Definition.Checksum ||
		ChecksumDefinition(original.Definition.DefinitionInput) != original.Definition.Checksum {
		return false
	}
	plan.SourceExecutionID = source.ExecutionID
	for _, persisted := range []*string{source.ScopeMax, original.ScopeMax} {
		if persisted == nil {
			continue
		}
		if plan.ScopeMax != nil && *plan.ScopeMax != *persisted {
			return false
		}
		plan.ScopeMax = persisted
	}
	if plan.Kind == ChangeResume {
		if plan.ReceiptExecutionID != source.ExecutionID || (original.Direction != DirectionUp && original.Direction != DirectionDown) {
			return false
		}
		plan.Direction = original.Direction
		plan.TargetExecutionID = original.TargetExecutionID
		if original.SourceExecutionID != "" {
			plan.SourceExecutionID = original.SourceExecutionID
		}
	} else {
		if plan.ReceiptExecutionID == source.ExecutionID || (original.Direction != DirectionUp && source.Status != JobPartiallyReverted) {
			return false
		}
		plan.Direction = DirectionDown
		if source.Status == JobPartiallyReverted {
			if original.Direction != DirectionDown || !security.ValidID(original.SourceExecutionID) {
				return false
			}
			plan.SourceExecutionID = original.SourceExecutionID
		}
	}
	return plan.Direction != DirectionDown || !plan.Definition.Irreversible
}
