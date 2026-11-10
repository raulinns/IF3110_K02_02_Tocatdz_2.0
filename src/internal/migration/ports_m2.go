package migration

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// K2 persistence ports are implemented by 13524044, consumed by 13524056.
// Methods return security's error categories (errors.Is), never driver errors.
// Every lookup includes DatabaseID. A valid ID from another DB is ErrNotFound.
// Reads alone DO NOT authorize callers. Application services enforce permissions.

type DefinitionStore interface {
	FindDefinition(ctx context.Context, databaseID, migrationID security.ID) (MigrationDefinition, error)
	ListDefinitions(ctx context.Context, databaseID security.ID, query PageQuery) (Page[MigrationDefinitionSummary], error)
}

type ChangeRequestStore interface {
	FindChangeRequest(ctx context.Context, databaseID, changeRequestID security.ID) (ChangeRequest, error)
	ListChangeRequests(ctx context.Context, databaseID security.ID, query ChangeRequestQuery) (Page[ChangeRequest], error)
}

type ExecutionStore interface {
	// ListExecutions filters rollback pairs before pagination; it never filters failures
	// out of jobs, which are a separate resource. Order is committed_at DESC, id DESC.
	ListExecutions(ctx context.Context, databaseID security.ID, query ExecutionQuery) (Page[CommittedExecutionSummary], error)
	FindExecution(ctx context.Context, databaseID, executionID security.ID) (Execution, error)
	ListExecutedStatements(ctx context.Context, databaseID, executionID security.ID, query PageQuery) (Page[ExecutedStatement], error)
	// ReadPosition returns active stack, redo chain and blockers in ONE consistent read.
	ReadPosition(ctx context.Context, databaseID security.ID) (Position, error)
}

type JobQueue interface {
	FindJob(ctx context.Context, databaseID, jobID security.ID) (JobView, error)
	ListJobs(ctx context.Context, databaseID security.ID, query JobQuery) (Page[JobView], error)
	ListAttempts(ctx context.Context, databaseID, jobID security.ID, query PageQuery) (Page[JobAttempt], error)
}

// JobLocator resolves only the database ID of /jobs/{job}; authenticate first,
// authorize job.view on that DB, then call FindJob. Never expose this result directly.
type JobLocator interface {
	JobDatabase(ctx context.Context, jobID security.ID) (security.ID, error)
}

type LeaderDirectory interface {
	RequiredApprovers(ctx context.Context, databaseID security.ID) ([]RequiredApproval, error)
	IsLeader(ctx context.Context, userID, groupID security.ID) (bool, error)
	LeaderGroups(ctx context.Context, userID security.ID) ([]security.ID, error)
}

// WorkflowStore makes definition/approval/queue mutations one transaction boundary.
// WithWorkflow locks the platform DB's per-database workflow row, rechecks active
// account/session/AuthVersion in that transaction and rolls back on ANY callback error.
// Grant/leader/membership mutations must use the same serialization protocol.
// Callbacks perform platform DB operations only: no target SQL or network calls.
type WorkflowStore interface {
	WithWorkflow(ctx context.Context, request Request, fn func(WorkflowTx) error) error
}

type WorkflowTx interface {
	// These reads share the same snapshot/lock as subsequent writes.
	Definition(migrationID security.ID) (MigrationDefinition, error)
	ChangeRequest(changeRequestID security.ID) (ChangeRequest, error)
	Position() (Position, error)
	Execution(executionID security.ID) (Execution, error)
	RequiredApprovers() ([]RequiredApproval, error)
	RequireAccess(access security.DatabaseAccess) error
	CreateDefinition(definition MigrationDefinition) error
	// Update fails if revision differs, definition frozen or an active CR references it.
	UpdateDefinition(migrationID security.ID, expectedRevision uint64, input DefinitionInput, checksum DefinitionChecksum, at time.Time) (MigrationDefinition, error)
	// Create freezes the definition and snapshots approvers/revision/checksum/position.
	// It rejects another open CR, except an explicit recovery of the blocking lineage.
	CreateChangeRequest(change ChangeRequest) error
	// RecordDecision verifies current group leadership and expected CR revision,
	// inserts one decision and computes rejected/approved atomically. No overwrites.
	// Exact replays return current state; changed decisions return ErrConflict.
	RecordDecision(changeRequestID security.ID, expectedRevision uint64, decision ApprovalDecision) (ChangeRequest, error)
	// EnqueueApproved rechecks approval/definition/position and writes job + queued
	// transition ATOMICALLY. UNIQUE(change_request_id) returns the existing job for
	// exact replays, even if it has since finished. It never executes target SQL.
	// Newly generated candidate IDs/times/trace context are ignored on an authorized
	// replay of the same CR; do not conflict merely because the retry generated IDs.
	EnqueueApproved(changeRequestID security.ID, expectedRevision uint64, spec JobSpec) (JobView, error)
}

// Revalidator (13524056) is called by worker (13524044) AFTER the cross-worker
// target lock is acquired. Job has a live lease, CR is queued; the plan verifies
// current requester account/grants (NOT a browser session), approver identities,
// checksums and position. Failure must dispatch ZERO target mutation statements.
type Revalidator interface {
	Revalidate(ctx context.Context, job JobView) (ExecutionPlan, error)
}

// RevalidationReader supplies a coherent snapshot under the platform workflow lock.
// Worker writes must repeat these CAS expectations when transitioning to running;
// backend auth/config changes serialize on the same database workflow lock.
type RevalidationReader interface {
	ReadRevalidation(ctx context.Context, databaseID, jobID security.ID) (RevalidationSnapshot, error)
}

type RevalidationSnapshot struct {
	Job              JobView
	ChangeRequest    ChangeRequest
	Definition       MigrationDefinition
	Position         Position
	Requester        security.Account
	RequesterAccess  security.DatabaseAccess
	CurrentApprovers []RequiredApproval
	SourceJob        *JobView
	SourcePlan       *ExecutionPlan // Persisted original direction/target/scope for chained recovery.
	TargetExecution  *Execution     // Exact original revision snapshot for rollback/forward.
}

// Runner is implemented by 13524044. LockedTarget is M1's normal SQL/receipt port.
// The batch runner owns a narrower extra capability interface for checkpoint/range
// SQL on that SAME physical locked connection. Missing capability must fail closed.
// Keep the advisory lock across ALL batch commits; never open an unlocked pool tx.
// Run returns proven results; worker persists history with CompleteCommitted.
// A non-nil Go error never proves rollback: retain RunResult partial/uncertain
// evidence and reconcile the target before choosing retry or final failure.
type Runner interface {
	Run(ctx context.Context, plan ExecutionPlan, target LockedTarget, progress ProgressSink) (RunResult, error)
}

// ProgressSink mirrors target-proven checkpoints, never speculative row counts.
// A lost mirror update is repaired from target checkpoints before retry/recovery.
type ProgressSink interface {
	BatchCommitted(ctx context.Context, checkpoint BatchCheckpoint) error
	BatchReverted(ctx context.Context, checkpoint BatchCheckpoint) error
}

// Worker-owned lifecycle ports explicitly fence stale workers. LeaseToken is
// monotonic per claim; renewing by owner name alone is insufficient after restart.
type JobLease struct {
	Job        JobView
	Owner      string
	Token      uint64
	LeaseUntil time.Time
}

type WorkerStore interface {
	ClaimJob(ctx context.Context, owner string, now time.Time, leaseDuration time.Duration) (JobLease, error)
	RenewLease(ctx context.Context, lease JobLease, until time.Time) (JobLease, error)
	// Start rechecks expectations, persists the immutable plan for recovery, and
	// sets first-dispatch metadata before target SQL. It never changes approved SQL.
	StartRunning(ctx context.Context, lease JobLease, plan ExecutionPlan) error
	// Completion atomically appends history, updates position, job/CR and attempt.
	// Execution.ID is unique; exact receipt replays are idempotent. An unknown target
	// commit is uncertain, NOT failed. Platform/target commits cannot be one tx.
	CompleteCommitted(ctx context.Context, lease JobLease, execution Execution) error
	// RecordOutcome persists an attempt even when no SQL was dispatched (e.g. a
	// revalidation failure). Incomplete committed work also installs a DB barrier.
	RecordOutcome(ctx context.Context, lease JobLease, result RunResult) error
	// Retry requires proof of no commit (or confirmed receipt reconciliation), bounded
	// attempts and backoff. Persist failed attempt + next run_after in one transaction.
	ScheduleRetry(ctx context.Context, lease JobLease, failure JobFailure, runAfter time.Time) error
}

// Recovery reads are scoped to the original receipt execution identity. Reverting
// uses reverse batch order and the exact committed scope, not today's WHERE result.
type CheckpointReader interface {
	Checkpoints(ctx context.Context, databaseID, executionID security.ID, query CheckpointQuery) (CheckpointPage, error)
}

// Keyset pagination remains stable as checkpoints are appended. Descending is
// mandatory for compensation. Before/After are exclusive batch numbers; zero is
// an unbounded initial cursor in the chosen direction, NOT a row-key cursor.
type CheckpointQuery struct {
	AfterBatch, BeforeBatch int64
	Limit                   int
	Descending              bool
}

type CheckpointPage struct {
	Items   []BatchCheckpoint
	HasNext bool // Next query uses last returned Batch as exclusive before/after cursor.
}

// Recovery is NOT a blind queued claim. It fences expired leases, checks target
// evidence under the target lock, then reconciles using the same completion ports.
type ReconciliationStore interface {
	ListRecoverable(ctx context.Context, query PageQuery) (Page[JobView], error)
	ClaimRecovery(ctx context.Context, databaseID, jobID security.ID, owner string, leaseDuration time.Duration) (JobLease, error)
}
