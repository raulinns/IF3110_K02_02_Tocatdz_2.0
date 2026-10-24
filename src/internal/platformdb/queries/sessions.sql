-- name: GetSessionByID :one
SELECT
    s.id, s.user_id, s.auth_version, s.revision, s.generation,
    s.csrf_epoch, s.created_at, s.last_seen_at, s.absolute_expires_at,
    s.rotated_at, s.current_digest, s.previous_digest,
    s.previous_valid_until, s.current_token_sealed, s.revoked_at,
    u.username, u.active, u.platform_admin, u.auth_version AS account_auth_version
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = $1;

-- name: GetSessionByDigest :one
SELECT
    s.id, s.user_id, s.auth_version, s.revision, s.generation,
    s.csrf_epoch, s.created_at, s.last_seen_at, s.absolute_expires_at,
    s.rotated_at, s.current_digest, s.previous_digest,
    s.previous_valid_until, s.current_token_sealed, s.revoked_at,
    u.username, u.active, u.platform_admin, u.auth_version AS account_auth_version,
    CASE
        WHEN s.previous_digest IS NULL THEN FALSE
        ELSE s.previous_digest = $1
    END::boolean AS presented_previous
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.current_digest = $1 OR s.previous_digest = $1
ORDER BY (s.revoked_at IS NULL) DESC,
         (s.previous_digest = $1) DESC
LIMIT 1;

-- name: InsertSession :exec
INSERT INTO sessions (
    id, user_id, auth_version, revision, generation, csrf_epoch,
    created_at, last_seen_at, absolute_expires_at, rotated_at,
    current_digest, previous_digest, previous_valid_until,
    current_token_sealed, revoked_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13,
    $14, $15
);

-- name: RevokeSessionByID :execrows
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, $2),
    revision = revision + CASE WHEN revoked_at IS NULL THEN 1 ELSE 0 END
WHERE id = $1;

-- name: RevokeUserSessionsExcept :exec
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, $3),
    revision = revision + CASE WHEN revoked_at IS NULL THEN 1 ELSE 0 END
WHERE user_id = $1
  AND id <> $2
  AND revoked_at IS NULL;

-- name: AdvanceSession :execrows
UPDATE sessions
SET auth_version = $4,
    revision = $5,
    generation = $6,
    csrf_epoch = $7,
    last_seen_at = $8,
    rotated_at = $9,
    current_digest = $10,
    previous_digest = $11,
    previous_valid_until = $12,
    current_token_sealed = $13,
    revoked_at = $14
WHERE sessions.id = $1
  AND sessions.revision = $2
  AND EXISTS (
      SELECT 1
      FROM users u
      WHERE u.id = sessions.user_id
        AND u.auth_version = $3
        AND u.active = TRUE
  )
  AND (
      sessions.current_digest = $15
      OR sessions.previous_digest = $15
  );
