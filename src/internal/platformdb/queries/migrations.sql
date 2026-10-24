-- name: NextMigrationVersion :one
SELECT (COALESCE(MAX(version), 0) + 1)::bigint AS next_version
FROM migration_history
WHERE database_id = $1;

-- name: CreateMigrationHistory :one
INSERT INTO migration_history (
    database_id, version, sql_text, executed_by, status
) VALUES ($1, $2, $3, $4, 'pending')
RETURNING id, database_id, version, sql_text, executed_by, status,
          created_at, executed_at, error_message;

-- name: MarkMigrationRunning :execrows
UPDATE migration_history
SET status = 'running',
    executed_at = CURRENT_TIMESTAMP,
    error_message = NULL
WHERE id = $1 AND status = 'pending';

-- name: FinishMigration :execrows
UPDATE migration_history
SET status = $2,
    executed_at = CURRENT_TIMESTAMP,
    error_message = $3
WHERE id = $1 AND status = 'running';

-- name: ListMigrationHistory :many
SELECT id, database_id, version, sql_text, executed_by, status,
       created_at, executed_at, error_message
FROM migration_history
WHERE database_id = $1
ORDER BY version DESC;
