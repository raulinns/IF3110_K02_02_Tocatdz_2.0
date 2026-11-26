package logging

import (
	"context"
	"log/slog"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Operation struct {
	Actor                                                        string
	DatabaseID, MigrationID, ChangeRequestID, JobID, ExecutionID security.ID
	Revision                                                     uint64
	Direction                                                    string
	Attempt                                                      int
	Batch                                                        int64
	Mutation                                                     bool
}

type operationKey struct{}

func WithOperation(ctx context.Context, op Operation) context.Context {
	return context.WithValue(ctx, operationKey{}, op)
}

func OperationFromContext(ctx context.Context) Operation {
	op, _ := ctx.Value(operationKey{}).(Operation)
	return op
}

func (op Operation) Attrs() []slog.Attr {
	actor := op.Actor
	if actor == "" {
		actor = "system"
	}
	return []slog.Attr{
		slog.String("actor", actor), slog.Any("database_id", optional(string(op.DatabaseID))),
		slog.Any("migration_id", optional(string(op.MigrationID))), slog.Any("change_request_id", optional(string(op.ChangeRequestID))),
		slog.Any("job_id", optional(string(op.JobID))), slog.Any("execution_id", optional(string(op.ExecutionID))),
		slog.Any("migration_revision", positive(op.Revision)), slog.Any("direction", optional(op.Direction)),
		slog.Any("attempt", positive(op.Attempt)), slog.Any("batch", positive(op.Batch)),
	}
}

func positive[T int | int64 | uint64](n T) any {
	if n <= 0 {
		return nil
	}
	return n
}
