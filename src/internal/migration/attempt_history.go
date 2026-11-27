package migration

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// HistoryEntry is an attempt, not a new position in the committed stack.
// Legacy committed entries without an attempt use their execution ID as ID.
type HistoryEntry struct {
	SnapshotAvailable                                                bool
	Checksum                                                         string
	ActorID                                                          security.ID
	ID, DatabaseID, MigrationID, ChangeRequestID, JobID, ExecutionID security.ID
	RetryOfJobID                                                     security.ID
	Attempt                                                          int
	Revision                                                         uint64
	Operation                                                        ChangeKind
	Direction                                                        ExecutionDirection
	Status                                                           JobStatus
	ResolvedStatus                                                   JobStatus
	Actor                                                            string
	StartedAt                                                        time.Time
	FinishedAt                                                       *time.Time
	CommittedAt                                                      *time.Time
	PositionVersion                                                  *PositionVersion
	RevertsExecutionID, ForwardOfExecutionID                         security.ID
	Error                                                            *JobFailure
	Definition                                                       *DefinitionInput // nil for legacy attempts whose snapshot was not captured.
	CanRetry                                                         bool
}

type HistoryStatement struct {
	ExecutedStatement
	Ordinal    int64
	Status     string // started means result unavailable, never a proven commit.
	SQLState   string
	StartedAt  time.Time
	FinishedAt *time.Time
}

type AttemptHistoryStore interface {
	ListHistory(context.Context, security.ID, ExecutionQuery) (Page[HistoryEntry], error)
	FindHistory(context.Context, security.ID, security.ID) (HistoryEntry, error)
	HistoryStatements(context.Context, security.ID, security.ID, PageQuery) (Page[HistoryStatement], error)
}

// StatementRecorder persists intent before driver dispatch and a separate result.
// Implementations must fence writes to the active job attempt/lease.
type StatementRecorder interface {
	StartStatement(context.Context, JobLease, ExecutedStatement) (int64, error)
	FinishStatement(context.Context, JobLease, int64, error) error
}
type statementRecorderKey struct{}
type statementRecording struct {
	recorder StatementRecorder
	lease    JobLease
}

func WithStatementRecorder(ctx context.Context, recorder StatementRecorder, lease JobLease) context.Context {
	return context.WithValue(ctx, statementRecorderKey{}, statementRecording{recorder, lease})
}

func StartAttemptStatement(ctx context.Context, statement ExecutedStatement) (func(error) error, error) {
	recording, ok := ctx.Value(statementRecorderKey{}).(statementRecording)
	if !ok {
		return func(error) error { return nil }, nil
	}
	id, err := recording.recorder.StartStatement(ctx, recording.lease, statement)
	if err != nil {
		return nil, err
	}
	return func(result error) error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), CleanupTimeout)
		defer cancel()
		return recording.recorder.FinishStatement(cleanup, recording.lease, id, result)
	}, nil
}
