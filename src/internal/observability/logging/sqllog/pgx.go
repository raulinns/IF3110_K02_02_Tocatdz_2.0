package sqllog

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Kind uint8

const (
	Platform Kind = iota
	Target
)

type Tracer struct{ Kind Kind }

var _ pgx.QueryTracer = Tracer{}

type queryKey struct{}

type query struct {
	started              time.Time
	logger               *slog.Logger
	end                  func(bool)
	attrs                []slog.Attr
	startEvent, endEvent string
}

func Start(ctx context.Context, kind Kind, sql security.Secret, parameterCount int) context.Context {
	ctx, end := tracing.Start(ctx, "db.query", tracing.Client)
	start, done := events(sql.Reveal(), kind, logging.OperationFromContext(ctx).Mutation)
	logger := logging.FromContext(ctx)
	if logger == nil {
		logger = slog.Default()
	}
	q := &query{started: time.Now(), logger: logger, end: end, startEvent: start, endEvent: done}
	if q.logger != nil && q.logger.Enabled(ctx, slog.LevelInfo) {
		op := logging.OperationFromContext(ctx)
		if op.Actor == "" {
			if p, ok := security.PrincipalFromContext(ctx); ok {
				op.Actor = string(p.UserID)
			}
		}
		redacted := migration.RedactStatement(sql)
		params := make([]string, min(max(parameterCount, 0), 200))
		for i := range params {
			params[i] = "[REDACTED]"
		}
		q.attrs = append(op.Attrs(), slog.String("query", redacted),
			slog.Bool("query_truncated", len(sql.Reveal()) > 16<<10),
			slog.Any("params", params), slog.Bool("params_truncated", parameterCount > len(params)))
		q.emit(ctx, q.startEvent, "start", nil, nil, nil, nil)
	}
	return context.WithValue(ctx, queryKey{}, q)
}

func Done(ctx context.Context, rows any, err error) {
	q, _ := ctx.Value(queryKey{}).(*query)
	if q == nil {
		return
	}
	defer q.end(err != nil)
	if q.attrs == nil {
		return
	}
	status := "succeeded"
	switch q.endEvent {
	case "tx.commit":
		status = "committed"
	case "tx.rollback":
		status = "rolled_back"
	}
	var failure, state any
	if err != nil {
		status, failure = "failed", "database query failed"
		if q.endEvent == "tx.commit" {
			status = "uncertain"
		}
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			status = "rolled_back"
		}
		rows = nil
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && validSQLState(pgErr.Code) {
			state = pgErr.Code
		}
	}
	q.emit(ctx, q.endEvent, status, float64(time.Since(q.started))/float64(time.Millisecond), rows, failure, state)
}

func (q *query) emit(ctx context.Context, event, status string, duration, rows, failure, state any) {
	attrs := append([]slog.Attr(nil), q.attrs...)
	attrs = append(attrs, slog.String("event", event), slog.String("status", status),
		slog.Any("duration_ms", duration), slog.Any("rows", rows), slog.Any("error", failure), slog.Any("sqlstate", state))
	q.logger.LogAttrs(ctx, slog.LevelInfo, event, attrs...)
}

func (t Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return Start(ctx, t.Kind, security.NewSecret(data.SQL), len(data.Args))
}

func (Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var rows any
	if data.CommandTag.String() != "" {
		rows = data.CommandTag.RowsAffected()
	}
	Done(ctx, rows, data.Err)
}

func events(sql string, kind Kind, mutation bool) (start, done string) {
	command := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	switch command {
	case "commit":
		return "tx.commit.start", "tx.commit"
	case "rollback":
		return "tx.rollback.start", "tx.rollback"
	}
	if kind == Target && mutation {
		if name, _, _ := strings.Cut(command, " "); name == "select" || name == "show" || name == "explain" {
			return "sql.query.start", "sql.query"
		}
		return "sql.exec.start", "sql.exec.done"
	}
	return "sql.query.start", "sql.query"
}

func validSQLState(code string) bool {
	if len(code) != 5 {
		return false
	}
	for _, ch := range code {
		if !(ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z') {
			return false
		}
	}
	return true
}
