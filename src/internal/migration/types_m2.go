package migration

import (
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// M2 types deliberately coexist with the M1 types. They are domain values, not
// HTTP DTOs: SQL/trace data must never be serialized by reflecting these structs.
const (
	MinBatchSize       = 100
	MaxBatchSize       = 100_000
	MaxApprovalNote    = 2000 // Unicode code points.
	MaxReasonRunes     = 2000
	DefaultMaxAttempts = 3
	MaxAttemptsLimit   = 10
)

type ExecutionMode string

const (
	ModeNormal ExecutionMode = "normal"
	ModeBatch  ExecutionMode = "batch"
)

type DefinitionState string

const (
	DefinitionDraft  DefinitionState = "draft"
	DefinitionFrozen DefinitionState = "frozen"
)

// BatchConfig is K4's UPDATE-only contract. UpSQL/DownSQL contain SET assignments,
// not full statements. Schema and table are separate identifiers, never SQL paths.
// The key is an immutable, non-null, unique PostgreSQL integer column.
type BatchConfig struct {
	Schema     string
	Table      string
	KeyColumn  string
	BatchSize  int
	ScopeWhere security.Secret // Optional restricted SQL expression, validated by runner.
}

type DefinitionInput struct {
	Title              string
	UpSQL              security.Secret
	DownSQL            security.Secret // Empty iff Irreversible.
	Irreversible       bool
	IrreversibleReason string
	Mode               ExecutionMode
	Batch              *BatchConfig // Non-nil iff ModeBatch.
}

// DefinitionChecksum is SHA-256, encoded as 64 lowercase hex characters in K1.
// Canonical input and versioning are specified in docs/contracts/m2.md.
type DefinitionChecksum [32]byte

type MigrationDefinition struct {
	ID, DatabaseID security.ID
	DefinitionInput
	State                DefinitionState
	Revision             uint64
	Checksum             DefinitionChecksum
	CreatedBy            security.ID
	CreatedAt, UpdatedAt time.Time
	FirstExecutionAt     *time.Time // First dispatch, including failed attempts; frozen at submit.
}

// List projection: never fetch SQL bodies for every row of a paginated list.
type MigrationDefinitionSummary struct {
	ID, DatabaseID       security.ID
	Title                string
	State                DefinitionState
	Revision             uint64
	Checksum             DefinitionChecksum
	Mode                 ExecutionMode
	Irreversible         bool
	CreatedBy            security.ID
	CreatedAt, UpdatedAt time.Time
	FirstExecutionAt     *time.Time
}

type ChangeKind string

const (
	ChangeApply         ChangeKind = "apply"
	ChangeRollback      ChangeKind = "rollback"
	ChangeForward       ChangeKind = "forward"
	ChangeResume        ChangeKind = "resume"
	ChangeRevertBatches ChangeKind = "revert_batches"
)

type CRStatus string

const (
	CRPendingReview     CRStatus = "pending_review"
	CRApproved          CRStatus = "approved"
	CRRejected          CRStatus = "rejected"
	CRQueued            CRStatus = "queued"
	CRRunning           CRStatus = "running"
	CRSucceeded         CRStatus = "succeeded"
	CRFailed            CRStatus = "failed"
	CRPartial           CRStatus = "partial"
	CRUncertain         CRStatus = "uncertain"
	CRPartiallyReverted CRStatus = "partially_reverted"
)

type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

type RequiredApproval struct {
	GroupID, LeaderUserID security.ID
	GroupName             string // Display snapshot; identity is always GroupID.
	Access                security.DatabaseAccess
}

type ApprovalDecision struct {
	GroupID, ActorID security.ID
	Decision         Decision
	Note             string
	DecidedAt        time.Time
}

type GroupApproval struct {
	RequiredApproval
	Decision *ApprovalDecision // Nil means awaiting a decision, never implicit approval.
}

type SubmitChange struct {
	MigrationID       security.ID
	MigrationRevision uint64
	Kind              ChangeKind
	TargetExecutionID security.ID // Required for rollback/forward; see K2.
	SourceJobID       security.ID // Required for resume/revert; optional apply only for an explicit repair.
}

type ChangeRequest struct {
	ID, DatabaseID security.ID
	SubmitChange
	Revision             uint64 // CAS token for approvals and enqueue.
	Checksum             DefinitionChecksum
	Status               CRStatus
	RequestedBy          security.ID
	Approvals            []GroupApproval
	ExpectedPosition     PositionVersion
	CreatedAt, UpdatedAt time.Time
}

type JobStatus string

const (
	JobQueued            JobStatus = "queued"
	JobRunning           JobStatus = "running"
	JobSucceeded         JobStatus = "succeeded"
	JobFailed            JobStatus = "failed"
	JobPartial           JobStatus = "partial"
	JobUncertain         JobStatus = "uncertain"
	JobPartiallyReverted JobStatus = "partially_reverted"
)

type ExecutionDirection string

const (
	DirectionUp   ExecutionDirection = "up"
	DirectionDown ExecutionDirection = "down"
)

type JobErrorCode string

const (
	JobErrorSQL          JobErrorCode = "sql_error"
	JobErrorConnection   JobErrorCode = "connection_error"
	JobErrorInterrupted  JobErrorCode = "interrupted"
	JobErrorRevalidation JobErrorCode = "revalidation_failed"
	JobErrorUncertain    JobErrorCode = "commit_uncertain"
	JobErrorUnsafeRevert JobErrorCode = "unsafe_revert"
)

// JobFailure contains only a safe category/message and SQLSTATE, never driver text.
type JobFailure struct {
	Code      JobErrorCode
	Message   string
	SQLState  string
	Retryable bool
}

// ExecutionID is allocated once per logical job, retained across retries/resume,
// and used for target receipts/checkpoints. A compensation has a new ExecutionID.
type JobSpec struct {
	ID, DatabaseID, ChangeRequestID, ExecutionID security.ID
	RequestedBy                                  security.ID
	MaxAttempts                                  int
	RequestID                                    string
	TraceContext                                 security.Secret // W3C carrier JSON; internal only.
	QueuedAt                                     time.Time
}

type JobAttempt struct {
	Attempt    int
	StartedAt  time.Time
	FinishedAt *time.Time
	Status     JobStatus
	Error      *JobFailure
}

type BatchProgress struct {
	BatchesCommitted int64
	RowsCommitted    int64
	BatchesReverted  int64
	RowsReverted     int64
	TotalBatches     *int64 // Nil if not known; never invent a percentage.
}

type JobView struct {
	JobSpec
	MigrationID           security.ID
	Kind                  ChangeKind
	Mode                  ExecutionMode
	Status                JobStatus
	Attempt               int
	RunAfter              time.Time
	StartedAt, FinishedAt *time.Time
	Partial               bool
	Progress              BatchProgress
	Error                 *JobFailure
	ScopeMax              *string // Internal frozen scope; excluded from K1 job DTO.
}

// Checkpoint ranges describe the original job's fixed scope. Cursor strings are
// signed decimal int64 values (not JS numbers); nil From means before the first row.
// The runner must preserve exact affected-key/recovery data when a range alone
// cannot safely identify the rows to compensate. It must reject unsafe reverts.
type BatchCheckpoint struct {
	ExecutionID       security.ID
	Batch             int64
	From              *string
	To, ScopeMax      string
	Rows              int64
	CommittedAt       time.Time
	RevertedAt        *time.Time
	RecoveryReference security.Secret
	Statement         ExecutedStatement // Persist with checkpoint; do not accumulate all batches in memory.
}

type PositionVersion uint64 // Monotonic per database; changes on position OR barrier updates.

// Position is read consistently. Active is oldest->newest; Forward is next->last.
// BlockedByJobID holds partial/uncertain recovery barriers, even if Active is empty.
type Position struct {
	Version        PositionVersion
	Active         []CommittedExecutionSummary
	Forward        []CommittedExecutionSummary
	BlockedByJobID security.ID
}

// ExecutedStatement is the actual statement sent to the driver, not just the
// migration template. Params require explicit redaction before logging/HTTP DTOs.
type ExecutedStatement struct {
	Query  security.Secret
	Params []security.Secret
	Batch  *int64
}

// Execution is appended only after a complete logical operation has committed.
// Partial work stays in checkpoints/jobs until completion or compensation.
// Backfilled M1 executions may have no JobID; all new M2 executions must have one.
type Execution struct {
	ID, DatabaseID, MigrationID, ChangeRequestID, JobID security.ID
	MigrationRevision                                   uint64
	Checksum                                            DefinitionChecksum
	Operation                                           ChangeKind
	Direction                                           ExecutionDirection
	RevertsExecutionID                                  security.ID
	ForwardOfExecutionID                                security.ID
	Definition                                          DefinitionInput     // Immutable revision snapshot.
	Statements                                          []ExecutedStatement // Normal mode; batch statements stream via checkpoints.
	ActorID                                             security.ID
	Actor                                               string
	StartedAt, CommittedAt                              time.Time
	PositionVersion                                     PositionVersion
}

type CommittedExecutionSummary struct {
	ID, DatabaseID, MigrationID, ChangeRequestID, JobID security.ID
	MigrationRevision                                   uint64
	Checksum                                            DefinitionChecksum
	Operation                                           ChangeKind
	Direction                                           ExecutionDirection
	RevertsExecutionID, ForwardOfExecutionID            security.ID
	ActorID                                             security.ID
	Actor                                               string
	StartedAt, CommittedAt                              time.Time
	PositionVersion                                     PositionVersion
}

// ExecutionPlan is authorized under the database lock and must not outlive it.
// Retry/resume keep ReceiptExecutionID; new rollback/forward/compensation get a new one.
type ExecutionPlan struct {
	JobID, ChangeRequestID, DatabaseID, MigrationID security.ID
	ReceiptExecutionID                              security.ID
	ActorID                                         security.ID
	Kind                                            ChangeKind
	Direction                                       ExecutionDirection
	Definition                                      MigrationDefinition
	ExpectedPosition                                PositionVersion
	TargetExecutionID, SourceJobID                  security.ID
	SourceExecutionID                               security.ID // Original checkpoint namespace for compensation.
	ScopeMax                                        *string
}

// PageQuery uses K1 page/limit defaults and bounds. Stable order includes an ID
// tie-breaker; adapters return an empty Items slice rather than nil in HTTP DTOs.
type PageQuery struct{ Page, Limit int }
type Page[T any] struct {
	Items       []T
	Page, Limit int
	HasNext     bool
}
type ChangeRequestQuery struct {
	PageQuery
	Status CRStatus // Empty means all.
}
type JobQuery struct {
	PageQuery
	Status JobStatus
}
type ExecutionQuery struct {
	PageQuery
	HideRollback bool // Pair over ALL executions before pagination.
}

type RunResult struct {
	Status    JobStatus
	Progress  BatchProgress
	Error     *JobFailure
	Execution *Execution // Non-nil only for completed, proven commits.
}
