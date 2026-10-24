package platformstore

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	platformsqlc "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb/sqlc"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type MigrationStatus string

const (
	MigrationPending    MigrationStatus = "pending"
	MigrationRunning    MigrationStatus = "running"
	MigrationCommitted  MigrationStatus = "committed"
	MigrationRolledBack MigrationStatus = "rolled_back"
	MigrationFailed     MigrationStatus = "failed"
)

type MigrationRecord struct {
	ID           int64
	DatabaseID   security.ID
	Version      int64
	SQL          string
	ExecutedBy   security.ID
	Status       MigrationStatus
	CreatedAt    time.Time
	ExecutedAt   *time.Time
	ErrorMessage string
}

func (s *Store) CreateMigration(
	ctx context.Context,
	databaseID security.ID,
	executedBy security.ID,
	sqlText string,
) (MigrationRecord, error) {
	if !security.ValidID(databaseID) || !security.ValidID(executedBy) || strings.TrimSpace(sqlText) == "" {
		return MigrationRecord{}, security.ErrInvalidInput
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MigrationRecord{}, repositoryError(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", string(databaseID)); err != nil {
		return MigrationRecord{}, repositoryError(err)
	}
	queries := s.queries.WithTx(tx)
	version, err := queries.NextMigrationVersion(ctx, string(databaseID))
	if err != nil {
		return MigrationRecord{}, repositoryError(err)
	}
	row, err := queries.CreateMigrationHistory(ctx, platformsqlc.CreateMigrationHistoryParams{
		DatabaseID: string(databaseID),
		Version:    version,
		SqlText:    sqlText,
		ExecutedBy: string(executedBy),
	})
	if err != nil {
		return MigrationRecord{}, repositoryError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MigrationRecord{}, repositoryError(err)
	}
	return migrationRecord(row)
}

func (s *Store) MarkMigrationRunning(ctx context.Context, id int64) error {
	if id <= 0 {
		return security.ErrInvalidInput
	}
	rows, err := s.queries.MarkMigrationRunning(ctx, id)
	if err != nil {
		return repositoryError(err)
	}
	if rows != 1 {
		return security.ErrConflict
	}
	return nil
}

func (s *Store) FinishMigration(
	ctx context.Context,
	id int64,
	status MigrationStatus,
	errorMessage string,
) error {
	if id <= 0 || !finalMigrationStatus(status) {
		return security.ErrInvalidInput
	}
	if status != MigrationFailed {
		errorMessage = ""
	}
	rows, err := s.queries.FinishMigration(ctx, platformsqlc.FinishMigrationParams{
		ID:     id,
		Status: string(status),
		ErrorMessage: pgtype.Text{
			String: errorMessage,
			Valid:  errorMessage != "",
		},
	})
	if err != nil {
		return repositoryError(err)
	}
	if rows != 1 {
		return security.ErrConflict
	}
	return nil
}

func (s *Store) ListMigrations(
	ctx context.Context,
	databaseID security.ID,
) ([]MigrationRecord, error) {
	if !security.ValidID(databaseID) {
		return nil, security.ErrInvalidInput
	}
	rows, err := s.queries.ListMigrationHistory(ctx, string(databaseID))
	if err != nil {
		return nil, repositoryError(err)
	}
	result := make([]MigrationRecord, 0, len(rows))
	for _, row := range rows {
		record, err := migrationRecord(row)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

func finalMigrationStatus(status MigrationStatus) bool {
	return status == MigrationCommitted || status == MigrationRolledBack || status == MigrationFailed
}

func migrationRecord(row platformsqlc.MigrationHistory) (MigrationRecord, error) {
	createdAt, err := requiredTimestamp(row.CreatedAt)
	if err != nil || row.ID <= 0 || row.Version <= 0 ||
		!security.ValidID(security.ID(row.DatabaseID)) ||
		!security.ValidID(security.ID(row.ExecutedBy)) ||
		!validMigrationStatus(MigrationStatus(row.Status)) {
		return MigrationRecord{}, security.ErrUnavailable
	}
	return MigrationRecord{
		ID:           row.ID,
		DatabaseID:   security.ID(row.DatabaseID),
		Version:      row.Version,
		SQL:          row.SqlText,
		ExecutedBy:   security.ID(row.ExecutedBy),
		Status:       MigrationStatus(row.Status),
		CreatedAt:    createdAt,
		ExecutedAt:   optionalTime(row.ExecutedAt),
		ErrorMessage: optionalText(row.ErrorMessage),
	}, nil
}

func validMigrationStatus(status MigrationStatus) bool {
	switch status {
	case MigrationPending, MigrationRunning, MigrationCommitted, MigrationRolledBack, MigrationFailed:
		return true
	default:
		return false
	}
}

func optionalText(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
