-- name: GetManagedDatabase :one
SELECT id, name, engine, connection_secret, active
FROM managed_databases
WHERE id = $1;

-- name: CreateManagedDatabase :exec
INSERT INTO managed_databases (
    id, name, engine, connection_secret, active
) VALUES ($1, $2, $3, $4, $5);

-- name: UpsertGroupDatabaseAccess :exec
INSERT INTO group_database_access (
    group_id, database_id, access_level
) VALUES ($1, $2, $3)
ON CONFLICT (group_id, database_id)
DO UPDATE SET
    access_level = EXCLUDED.access_level,
    updated_at = CURRENT_TIMESTAMP;
