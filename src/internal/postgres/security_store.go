package postgres

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Store adalah repository platform: user, session, grant, dan direktori database target.
type Store struct {
	pool  *pgxpool.Pool
	clock security.Clock
}

var (
	_ security.CredentialRepository = (*Store)(nil)
	_ security.SessionRepository    = (*Store)(nil)
	_ security.AccessRepository     = (*Store)(nil)
	_ explorer.Directory            = (*Store)(nil)
	_ migration.Accounts            = (*Store)(nil)
)

func NewStore(pool *pgxpool.Pool, clock security.Clock) (*Store, error) {
	if pool == nil || clock == nil {
		return nil, security.ErrInvalidInput
	}
	return &Store{pool: pool, clock: clock}, nil
}

var readTx = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

const accountColumns = "id::text, username, active, platform_admin, auth_version"

func scanAccount(row pgx.Row, extra ...any) (security.Account, error) {
	var account security.Account
	var id string
	var version int64
	targets := append([]any{&id, &account.Username, &account.Active, &account.PlatformAdmin, &version}, extra...)
	if err := row.Scan(targets...); err != nil {
		return security.Account{}, mapError(err)
	}
	account.ID, account.AuthVersion = security.ID(id), uint64(version)
	return account, nil
}

func findAccount(ctx context.Context, q querier, id security.ID, suffix string) (security.Account, error) {
	if !security.ValidID(id) {
		return security.Account{}, security.ErrNotFound
	}
	return scanAccount(q.QueryRow(ctx, "SELECT "+accountColumns+" FROM users WHERE id = $1"+suffix, string(id)))
}

func (s *Store) FindByUsername(ctx context.Context, username string) (security.CredentialRecord, error) {
	var hash string
	account, err := scanAccount(s.pool.QueryRow(ctx, "SELECT "+accountColumns+", password_hash FROM users WHERE username = $1", username), &hash)
	if err != nil {
		return security.CredentialRecord{}, err
	}
	return security.CredentialRecord{Account: account, PasswordHash: security.NewSecret(hash)}, nil
}

func (s *Store) FindAccount(ctx context.Context, id security.ID) (security.Account, error) {
	return findAccount(ctx, s.pool, id, "")
}

func (s *Store) Rehash(ctx context.Context, id security.ID, expected, replacement security.Secret, version uint64) error {
	tag, err := s.pool.Exec(ctx, "UPDATE users SET password_hash = $3 WHERE id = $1 AND password_hash = $2 AND auth_version = $4",
		string(id), expected.Reveal(), replacement.Reveal(), int64(version))
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return security.ErrConflict
	}
	return nil
}

// Akun dikunci berurutan UUID sebelum session, sama seperti mutasi admin, supaya tidak deadlock.
func lockAccounts(ctx context.Context, tx pgx.Tx, users ...security.ID) (map[security.ID]security.Account, error) {
	rows, err := tx.Query(ctx, "SELECT "+accountColumns+" FROM users WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE", ids(users))
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := map[security.ID]security.Account{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		result[account.ID] = account
	}
	return result, mapError(rows.Err())
}

const sessionColumns = `id::text, user_id::text, auth_version, revision, generation, csrf_epoch, created_at, last_seen_at,
	absolute_expires_at, rotated_at, current_token_sealed, previous_valid_until, revoked_at`

func loadSession(ctx context.Context, q querier, id security.ID, suffix string) (security.Session, error) {
	var session security.Session
	var sessionID, userID, sealed string
	var authVersion, revision, generation, epoch int64
	var previousUntil, revoked *time.Time
	err := q.QueryRow(ctx, "SELECT "+sessionColumns+" FROM sessions WHERE id = $1"+suffix, string(id)).Scan(&sessionID, &userID, &authVersion,
		&revision, &generation, &epoch, &session.CreatedAt, &session.LastSeenAt, &session.AbsoluteExpiresAt, &session.RotatedAt, &sealed, &previousUntil, &revoked)
	if err != nil {
		return security.Session{}, mapError(err)
	}
	session.ID, session.UserID, session.CurrentTokenSealed = security.ID(sessionID), security.ID(userID), security.NewSecret(sealed)
	session.AuthVersion, session.Revision, session.Generation, session.CSRFEpoch = uint64(authVersion), uint64(revision), uint64(generation), uint64(epoch)
	session.CreatedAt, session.LastSeenAt, session.AbsoluteExpiresAt, session.RotatedAt = utc(session.CreatedAt), utc(session.LastSeenAt), utc(session.AbsoluteExpiresAt), utc(session.RotatedAt)
	if previousUntil != nil {
		session.PreviousValidUntil = utc(*previousUntil)
	}
	if revoked != nil {
		value := utc(*revoked)
		session.RevokedAt = &value
	}
	rows, err := q.Query(ctx, "SELECT digest, slot FROM session_tokens WHERE session_id = $1", string(id))
	if err != nil {
		return security.Session{}, mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var digest []byte
		var slot string
		if err := rows.Scan(&digest, &slot); err != nil {
			return security.Session{}, mapError(err)
		}
		if len(digest) != len(security.TokenDigest{}) {
			return security.Session{}, security.ErrUnavailable
		}
		value := security.TokenDigest(digest)
		if slot == "current" {
			session.CurrentDigest = value
		} else {
			session.PreviousDigest = &value
		}
	}
	return session, mapError(rows.Err())
}

func tokenOwner(ctx context.Context, q querier, digest security.TokenDigest) (security.ID, bool, error) {
	var id, slot string
	if err := q.QueryRow(ctx, "SELECT session_id::text, slot FROM session_tokens WHERE digest = $1", digest[:]).Scan(&id, &slot); err != nil {
		return "", false, mapError(err)
	}
	return security.ID(id), slot == "previous", nil
}

func (s *Store) snapshot(ctx context.Context, sessionID security.ID, previous bool) (security.SessionSnapshot, error) {
	var snapshot security.SessionSnapshot
	err := withTx(ctx, s.pool, readTx, func(tx pgx.Tx) error {
		session, err := loadSession(ctx, tx, sessionID, "")
		if err != nil {
			return err
		}
		account, err := findAccount(ctx, tx, session.UserID, "")
		if err != nil {
			return err
		}
		snapshot = security.SessionSnapshot{Session: session, Account: account, PresentedPrevious: previous}
		return nil
	})
	return snapshot, err
}

func (s *Store) Lookup(ctx context.Context, digest security.TokenDigest) (security.SessionSnapshot, error) {
	id, previous, err := tokenOwner(ctx, s.pool, digest)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	return s.snapshot(ctx, id, previous)
}

func (s *Store) FindByID(ctx context.Context, id security.ID) (security.SessionSnapshot, error) {
	if !security.ValidID(id) {
		return security.SessionSnapshot{}, security.ErrNotFound
	}
	return s.snapshot(ctx, id, false)
}

func minTime(values ...time.Time) time.Time {
	result := values[0]
	for _, value := range values[1:] {
		if value.Before(result) {
			result = value
		}
	}
	return result
}

// PostgreSQL menyimpan mikrodetik; waktu dipotong sebelum ditulis supaya perbandingan deadline tetap konsisten.
func micro(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = micro(value)
	return &value
}

func writeTokens(ctx context.Context, tx pgx.Tx, session security.Session) error {
	if _, err := tx.Exec(ctx, "DELETE FROM session_tokens WHERE session_id = $1", string(session.ID)); err != nil {
		return mapError(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO session_tokens (digest, session_id, slot) VALUES ($1, $2, 'current')", session.CurrentDigest[:], string(session.ID)); err != nil {
		return mapError(err)
	}
	if session.PreviousDigest != nil {
		if _, err := tx.Exec(ctx, "INSERT INTO session_tokens (digest, session_id, slot) VALUES ($1, $2, 'previous')", session.PreviousDigest[:], string(session.ID)); err != nil {
			return mapError(err)
		}
	}
	return nil
}

func (s *Store) Create(ctx context.Context, session security.Session, version uint64, previous *security.TokenDigest, policy security.SessionPolicy) error {
	if !security.ValidID(session.ID) || !security.ValidID(session.UserID) || session.PreviousDigest != nil {
		return security.ErrInvalidInput
	}
	session.CreatedAt, session.LastSeenAt, session.RotatedAt, session.AbsoluteExpiresAt = micro(session.CreatedAt), micro(session.LastSeenAt), micro(session.RotatedAt), micro(session.AbsoluteExpiresAt)
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		users := []security.ID{session.UserID}
		var oldID security.ID
		var wasPrevious bool
		if previous != nil {
			id, slot, err := tokenOwner(ctx, tx, *previous)
			if err != nil && !errors.Is(err, security.ErrNotFound) {
				return err
			}
			if err == nil {
				oldID, wasPrevious = id, slot
				old, err := loadSession(ctx, tx, oldID, "")
				if err != nil {
					return err
				}
				users = append(users, old.UserID)
			}
		}
		accounts, err := lockAccounts(ctx, tx, users...)
		if err != nil {
			return err
		}
		account, ok := accounts[session.UserID]
		if !ok || !account.Active || account.AuthVersion != version {
			return security.ErrConflict
		}
		if oldID != "" {
			old, err := loadSession(ctx, tx, oldID, " FOR UPDATE")
			if err != nil {
				return err
			}
			now := micro(s.clock.Now())
			oldAccount := accounts[old.UserID]
			eligible := old.RevokedAt == nil && old.UserID == session.UserID && oldAccount.Active && old.AuthVersion == version &&
				now.Before(old.AbsoluteExpiresAt) && now.Before(old.LastSeenAt.Add(policy.IdleTimeout))
			// Deadline juga dibatasi RotatedAt session baru agar grace tidak melampaui kebijakan session itu sendiri.
			deadline := minTime(now.Add(policy.GracePeriod), old.AbsoluteExpiresAt, old.LastSeenAt.Add(policy.IdleTimeout), session.RotatedAt.Add(policy.GracePeriod), session.AbsoluteExpiresAt)
			if wasPrevious {
				deadline = minTime(deadline, old.PreviousValidUntil)
			}
			if eligible && now.Before(deadline) && deadline.After(session.RotatedAt) {
				value := *previous
				session.PreviousDigest, session.PreviousValidUntil = &value, micro(deadline)
				if _, err := tx.Exec(ctx, "DELETE FROM session_tokens WHERE digest = $1", previous[:]); err != nil {
					return mapError(err)
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = COALESCE(revoked_at, $2), revision = revision + 1,
				previous_valid_until = CASE WHEN $3 THEN NULL ELSE previous_valid_until END WHERE id = $1`, string(oldID), now, wasPrevious && session.PreviousDigest != nil); err != nil {
				return mapError(err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sessions (id, user_id, auth_version, revision, generation, csrf_epoch, created_at, last_seen_at,
			absolute_expires_at, rotated_at, current_token_sealed, previous_valid_until) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			string(session.ID), string(session.UserID), int64(session.AuthVersion), int64(session.Revision), int64(session.Generation), int64(session.CSRFEpoch),
			session.CreatedAt, session.LastSeenAt, session.AbsoluteExpiresAt, session.RotatedAt, session.CurrentTokenSealed.Reveal(), nullableTime(session.PreviousValidUntil)); err != nil {
			return mapError(err)
		}
		return writeTokens(ctx, tx, session)
	})
}

func (s *Store) Advance(ctx context.Context, presented security.TokenDigest, replacement security.Session, revision, accountVersion uint64, policy security.SessionPolicy) error {
	if !security.ValidID(replacement.ID) || !security.ValidID(replacement.UserID) {
		return security.ErrInvalidInput
	}
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		accounts, err := lockAccounts(ctx, tx, replacement.UserID)
		if err != nil {
			return err
		}
		current, err := loadSession(ctx, tx, replacement.ID, " FOR UPDATE")
		if errors.Is(err, security.ErrNotFound) {
			return security.ErrConflict
		}
		if err != nil {
			return err
		}
		account, ok := accounts[current.UserID]
		// Waktu dibaca setelah lock supaya pemeriksaan deadline memakai kondisi terbaru.
		now := s.clock.Now().UTC()
		if !ok || current.Revision != revision || !account.Active || account.AuthVersion != accountVersion || current.RevokedAt != nil ||
			!now.Before(current.LastSeenAt.Add(policy.IdleTimeout)) || !now.Before(current.AbsoluteExpiresAt) {
			return security.ErrConflict
		}
		currentToken := current.CurrentDigest == presented
		previousToken := current.PreviousDigest != nil && *current.PreviousDigest == presented && now.Before(current.PreviousValidUntil)
		if !currentToken && !previousToken {
			return security.ErrConflict
		}
		replacement.LastSeenAt, replacement.RotatedAt, replacement.AbsoluteExpiresAt, replacement.CreatedAt =
			micro(replacement.LastSeenAt), micro(replacement.RotatedAt), micro(replacement.AbsoluteExpiresAt), micro(replacement.CreatedAt)
		if replacement.UserID != current.UserID || !replacement.CreatedAt.Equal(current.CreatedAt) || !replacement.AbsoluteExpiresAt.Equal(current.AbsoluteExpiresAt) ||
			replacement.LastSeenAt.Before(current.LastSeenAt) || replacement.AuthVersion != accountVersion || replacement.RevokedAt != nil {
			return security.ErrInvalidInput
		}
		replacement.Revision = current.Revision + 1
		if _, err := tx.Exec(ctx, `UPDATE sessions SET auth_version = $2, revision = $3, generation = $4, csrf_epoch = $5, last_seen_at = $6,
			rotated_at = $7, current_token_sealed = $8, previous_valid_until = $9 WHERE id = $1`,
			string(replacement.ID), int64(replacement.AuthVersion), int64(replacement.Revision), int64(replacement.Generation), int64(replacement.CSRFEpoch),
			replacement.LastSeenAt, replacement.RotatedAt, replacement.CurrentTokenSealed.Reveal(), nullableTime(replacement.PreviousValidUntil)); err != nil {
			return mapError(err)
		}
		return writeTokens(ctx, tx, replacement)
	})
}

func (s *Store) Revoke(ctx context.Context, id security.ID, now time.Time) error {
	if !security.ValidID(id) {
		return security.ErrInvalidInput
	}
	_, err := s.pool.Exec(ctx, "UPDATE sessions SET revoked_at = $2, revision = revision + 1 WHERE id = $1 AND revoked_at IS NULL", string(id), micro(now))
	return mapError(err)
}

func grantsFor(ctx context.Context, q querier, user, database security.ID) ([]security.DatabaseAccess, error) {
	rows, err := q.Query(ctx, `SELECT g.access FROM group_database_grants g JOIN group_memberships m ON m.group_id = g.group_id
		WHERE m.user_id = $1 AND g.database_id = $2`, string(user), string(database))
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []security.DatabaseAccess
	for rows.Next() {
		var access string
		if err := rows.Scan(&access); err != nil {
			return nil, mapError(err)
		}
		result = append(result, security.DatabaseAccess(access))
	}
	return result, mapError(rows.Err())
}

func (s *Store) AccessForDatabase(ctx context.Context, user, database security.ID) (security.AccessSnapshot, error) {
	if !security.ValidID(user) || !security.ValidID(database) {
		return security.AccessSnapshot{}, security.ErrNotFound
	}
	var snapshot security.AccessSnapshot
	err := withTx(ctx, s.pool, readTx, func(tx pgx.Tx) error {
		account, err := findAccount(ctx, tx, user, "")
		if err != nil {
			return err
		}
		grants, err := grantsFor(ctx, tx, user, database)
		if err != nil {
			return err
		}
		snapshot = security.AccessSnapshot{Account: account, GroupAccess: grants}
		return nil
	})
	return snapshot, err
}

// Daftar dan akun dibaca dari snapshot transaksi yang sama, jadi epoch yang berubah di tengah jalan terdeteksi.
func (s *Store) ListAccessible(ctx context.Context, principal security.Principal) ([]explorer.DatabaseSummary, error) {
	var result []explorer.DatabaseSummary
	err := withTx(ctx, s.pool, readTx, func(tx pgx.Tx) error {
		account, err := findAccount(ctx, tx, principal.UserID, "")
		if errors.Is(err, security.ErrNotFound) {
			return security.ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		if !account.Active || account.AuthVersion != principal.AuthVersion {
			return security.ErrUnauthenticated
		}
		rows, err := tx.Query(ctx, `SELECT d.id::text, d.name, array_agg(g.access) FROM managed_databases d
			JOIN group_database_grants g ON g.database_id = d.id JOIN group_memberships m ON m.group_id = g.group_id
			WHERE m.user_id = $1 GROUP BY d.id, d.name`, string(principal.UserID))
		if err != nil {
			return mapError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			var grants []string
			if err := rows.Scan(&id, &name, &grants); err != nil {
				return mapError(err)
			}
			levels := make([]security.DatabaseAccess, len(grants))
			for i, grant := range grants {
				levels[i] = security.DatabaseAccess(grant)
			}
			access, err := security.EffectiveAccess(levels)
			if err != nil {
				return security.ErrUnavailable
			}
			result = append(result, explorer.DatabaseSummary{ID: security.ID(id), Name: name, Access: access})
		}
		return mapError(rows.Err())
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, err
}
