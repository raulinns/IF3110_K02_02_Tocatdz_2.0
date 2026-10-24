-- name: GetUserByUsername :one
SELECT id, username, password_hash, active, platform_admin, auth_version
FROM users
WHERE username = $1;

-- name: GetUserByID :one
SELECT id, username, password_hash, active, platform_admin, auth_version
FROM users
WHERE id = $1;

-- name: RehashUserPassword :execrows
UPDATE users
SET password_hash = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND password_hash = $2
  AND auth_version = $4;

-- name: CreateUser :exec
INSERT INTO users (
    id, username, password_hash, active, platform_admin, auth_version
) VALUES ($1, $2, $3, $4, $5, $6);
