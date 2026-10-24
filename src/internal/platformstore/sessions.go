package platformstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	platformsqlc "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb/sqlc"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type sessionRow struct {
	id                 string
	userID             string
	authVersion        int64
	revision           int64
	generation         int64
	csrfEpoch          int64
	createdAt          pgtype.Timestamptz
	lastSeenAt         pgtype.Timestamptz
	absoluteExpiresAt  pgtype.Timestamptz
	rotatedAt          pgtype.Timestamptz
	currentDigest      []byte
	previousDigest     []byte
	previousValidUntil pgtype.Timestamptz
	currentTokenSealed string
	revokedAt          pgtype.Timestamptz
	username           string
	active             bool
	platformAdmin      bool
	accountAuthVersion int64
	presentedPrevious  bool
}

func requiredTimestamp(value pgtype.Timestamptz) (time.Time, error) {
	if !value.Valid || value.InfinityModifier != pgtype.Finite {
		return time.Time{}, security.ErrUnavailable
	}
	return value.Time.UTC(), nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid || value.InfinityModifier != pgtype.Finite {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func digest(value []byte) (security.TokenDigest, error) {
	var result security.TokenDigest
	if len(value) != len(result) {
		return result, security.ErrUnavailable
	}
	copy(result[:], value)
	return result, nil
}

func digestBytes(value security.TokenDigest) []byte {
	result := make([]byte, len(value))
	copy(result, value[:])
	return result
}

func optionalDigestBytes(value *security.TokenDigest) []byte {
	if value == nil {
		return nil
	}
	return digestBytes(*value)
}

func snapshotFromRow(row sessionRow) (security.SessionSnapshot, error) {
	authVersion, err := checkedUint64(row.authVersion)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	revision, err := checkedUint64(row.revision)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	generation, err := checkedUint64(row.generation)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	csrfEpoch, err := checkedUint64(row.csrfEpoch)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	createdAt, err := requiredTimestamp(row.createdAt)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	lastSeenAt, err := requiredTimestamp(row.lastSeenAt)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	absoluteExpiresAt, err := requiredTimestamp(row.absoluteExpiresAt)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	rotatedAt, err := requiredTimestamp(row.rotatedAt)
	if err != nil {
		return security.SessionSnapshot{}, err
	}
	currentDigest, err := digest(row.currentDigest)
	if err != nil {
		return security.SessionSnapshot{}, err
	}

	var previousDigest *security.TokenDigest
	previousValidUntil := time.Time{}
	if len(row.previousDigest) > 0 {
		value, err := digest(row.previousDigest)
		if err != nil || !row.previousValidUntil.Valid {
			return security.SessionSnapshot{}, security.ErrUnavailable
		}
		previousDigest = &value
		previousValidUntil, err = requiredTimestamp(row.previousValidUntil)
		if err != nil {
			return security.SessionSnapshot{}, err
		}
	} else if row.previousValidUntil.Valid {
		return security.SessionSnapshot{}, security.ErrUnavailable
	}

	account, err := account(row.userID, row.username, row.active, row.platformAdmin, row.accountAuthVersion)
	if err != nil || !security.ValidID(security.ID(row.id)) {
		return security.SessionSnapshot{}, security.ErrUnavailable
	}
	return security.SessionSnapshot{
		Session: security.Session{
			ID:                 security.ID(row.id),
			UserID:             security.ID(row.userID),
			AuthVersion:        authVersion,
			Revision:           revision,
			Generation:         generation,
			CSRFEpoch:          csrfEpoch,
			CreatedAt:          createdAt,
			LastSeenAt:         lastSeenAt,
			AbsoluteExpiresAt:  absoluteExpiresAt,
			RotatedAt:          rotatedAt,
			CurrentDigest:      currentDigest,
			PreviousDigest:     previousDigest,
			PreviousValidUntil: previousValidUntil,
			CurrentTokenSealed: security.NewSecret(row.currentTokenSealed),
			RevokedAt:          optionalTime(row.revokedAt),
		},
		Account:           account,
		PresentedPrevious: row.presentedPrevious,
	}, nil
}

func sessionRowByID(row platformsqlc.GetSessionByIDRow) sessionRow {
	return sessionRow{
		id: row.ID, userID: row.UserID, authVersion: row.AuthVersion,
		revision: row.Revision, generation: row.Generation, csrfEpoch: row.CsrfEpoch,
		createdAt: row.CreatedAt, lastSeenAt: row.LastSeenAt,
		absoluteExpiresAt: row.AbsoluteExpiresAt, rotatedAt: row.RotatedAt,
		currentDigest: row.CurrentDigest, previousDigest: row.PreviousDigest,
		previousValidUntil: row.PreviousValidUntil, currentTokenSealed: row.CurrentTokenSealed,
		revokedAt: row.RevokedAt, username: row.Username, active: row.Active,
		platformAdmin: row.PlatformAdmin, accountAuthVersion: row.AccountAuthVersion,
	}
}

func sessionRowByDigest(row platformsqlc.GetSessionByDigestRow) sessionRow {
	result := sessionRowByID(platformsqlc.GetSessionByIDRow{
		ID: row.ID, UserID: row.UserID, AuthVersion: row.AuthVersion,
		Revision: row.Revision, Generation: row.Generation, CsrfEpoch: row.CsrfEpoch,
		CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt,
		AbsoluteExpiresAt: row.AbsoluteExpiresAt, RotatedAt: row.RotatedAt,
		CurrentDigest: row.CurrentDigest, PreviousDigest: row.PreviousDigest,
		PreviousValidUntil: row.PreviousValidUntil, CurrentTokenSealed: row.CurrentTokenSealed,
		RevokedAt: row.RevokedAt, Username: row.Username, Active: row.Active,
		PlatformAdmin: row.PlatformAdmin, AccountAuthVersion: row.AccountAuthVersion,
	})
	result.presentedPrevious = row.PresentedPrevious
	return result
}

func (s *Store) Lookup(ctx context.Context, value security.TokenDigest) (security.SessionSnapshot, error) {
	row, err := s.queries.GetSessionByDigest(ctx, digestBytes(value))
	if err != nil {
		return security.SessionSnapshot{}, repositoryError(err)
	}
	return snapshotFromRow(sessionRowByDigest(row))
}

func (s *Store) FindByID(ctx context.Context, sessionID security.ID) (security.SessionSnapshot, error) {
	if !security.ValidID(sessionID) {
		return security.SessionSnapshot{}, security.ErrInvalidInput
	}
	row, err := s.queries.GetSessionByID(ctx, string(sessionID))
	if err != nil {
		return security.SessionSnapshot{}, repositoryError(err)
	}
	return snapshotFromRow(sessionRowByID(row))
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func insertSessionParams(value security.Session) (platformsqlc.InsertSessionParams, error) {
	authVersion, err := checkedInt64(value.AuthVersion)
	if err != nil {
		return platformsqlc.InsertSessionParams{}, err
	}
	revision, err := checkedInt64(value.Revision)
	if err != nil {
		return platformsqlc.InsertSessionParams{}, err
	}
	generation, err := checkedInt64(value.Generation)
	if err != nil {
		return platformsqlc.InsertSessionParams{}, err
	}
	csrfEpoch, err := checkedInt64(value.CSRFEpoch)
	if err != nil {
		return platformsqlc.InsertSessionParams{}, err
	}
	return platformsqlc.InsertSessionParams{
		ID: string(value.ID), UserID: string(value.UserID), AuthVersion: authVersion,
		Revision: revision, Generation: generation, CsrfEpoch: csrfEpoch,
		CreatedAt: timestamp(value.CreatedAt), LastSeenAt: timestamp(value.LastSeenAt),
		AbsoluteExpiresAt: timestamp(value.AbsoluteExpiresAt), RotatedAt: timestamp(value.RotatedAt),
		CurrentDigest: digestBytes(value.CurrentDigest), PreviousDigest: optionalDigestBytes(value.PreviousDigest),
		PreviousValidUntil: timestamp(value.PreviousValidUntil), CurrentTokenSealed: value.CurrentTokenSealed.Reveal(),
		RevokedAt: func() pgtype.Timestamptz {
			if value.RevokedAt == nil {
				return pgtype.Timestamptz{}
			}
			return timestamp(*value.RevokedAt)
		}(),
	}, nil
}

func (s *Store) Create(
	ctx context.Context,
	value security.Session,
	expectedAuthVersion uint64,
	previous *security.TokenDigest,
	policy security.SessionPolicy,
) error {
	version, err := checkedInt64(expectedAuthVersion)
	if err != nil || policy.Validate() != nil || !security.ValidID(value.ID) || !security.ValidID(value.UserID) {
		return security.ErrInvalidInput
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return repositoryError(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var active bool
	var accountVersion int64
	err = tx.QueryRow(ctx, "SELECT active, auth_version FROM users WHERE id = $1 FOR UPDATE", string(value.UserID)).Scan(&active, &accountVersion)
	if err != nil || !active || accountVersion != version {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return repositoryError(err)
		}
		return security.ErrConflict
	}

	queries := platformsqlc.New(tx)
	if previous != nil {
		var oldID string
		err = tx.QueryRow(ctx, `
SELECT id FROM sessions
WHERE current_digest = $1 OR previous_digest = $1
ORDER BY (revoked_at IS NULL) DESC, (previous_digest = $1) DESC
LIMIT 1 FOR UPDATE`, digestBytes(*previous)).Scan(&oldID)
		if err == nil {
			oldRow, readErr := queries.GetSessionByID(ctx, oldID)
			if readErr != nil {
				return repositoryError(readErr)
			}
			old, mapErr := snapshotFromRow(sessionRowByID(oldRow))
			if mapErr != nil {
				return mapErr
			}
			now := value.CreatedAt.UTC()
			wasPrevious := old.Session.PreviousDigest != nil && *old.Session.PreviousDigest == *previous
			eligible := old.Session.RevokedAt == nil && old.Session.UserID == value.UserID && old.Account.Active &&
				old.Account.AuthVersion == expectedAuthVersion && now.Before(old.Session.AbsoluteExpiresAt) &&
				now.Before(old.Session.LastSeenAt.Add(policy.IdleTimeout))
			deadline := minTime(now.Add(policy.GracePeriod), minTime(old.Session.AbsoluteExpiresAt, old.Session.LastSeenAt.Add(policy.IdleTimeout)))
			if wasPrevious {
				deadline = minTime(deadline, old.Session.PreviousValidUntil)
			}
			if eligible && now.Before(deadline) {
				copied := *previous
				value.PreviousDigest = &copied
				value.PreviousValidUntil = deadline
			}
			_, err = tx.Exec(ctx, `
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, $2),
    revision = revision + CASE WHEN revoked_at IS NULL THEN 1 ELSE 0 END,
    previous_digest = NULL,
    previous_valid_until = NULL
WHERE id = $1`, oldID, now)
			if err != nil {
				return repositoryError(err)
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return repositoryError(err)
		}
	}

	params, err := insertSessionParams(value)
	if err != nil {
		return err
	}
	if err := queries.InsertSession(ctx, params); err != nil {
		return repositoryError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repositoryError(err)
	}
	return nil
}

func (s *Store) Advance(
	ctx context.Context,
	presented security.TokenDigest,
	replacement security.Session,
	expectedRevision uint64,
	expectedAccountVersion uint64,
	policy security.SessionPolicy,
) error {
	revision, err := checkedInt64(expectedRevision)
	if err != nil || policy.Validate() != nil || !security.ValidID(replacement.ID) {
		return security.ErrInvalidInput
	}
	accountVersion, err := checkedInt64(expectedAccountVersion)
	if err != nil {
		return err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return repositoryError(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var lockedID string
	err = tx.QueryRow(ctx, `
SELECT s.id
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = $1
FOR UPDATE OF s, u`, string(replacement.ID)).Scan(&lockedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return security.ErrNotFound
		}
		return repositoryError(err)
	}

	queries := platformsqlc.New(tx)
	row, err := queries.GetSessionByID(ctx, lockedID)
	if err != nil {
		return repositoryError(err)
	}
	current, err := snapshotFromRow(sessionRowByID(row))
	if err != nil {
		return err
	}
	now := replacement.LastSeenAt.UTC()
	currentToken := current.Session.CurrentDigest == presented
	previousToken := current.Session.PreviousDigest != nil && *current.Session.PreviousDigest == presented && now.Before(current.Session.PreviousValidUntil)
	if current.Session.Revision != expectedRevision || !current.Account.Active || current.Account.AuthVersion != expectedAccountVersion ||
		current.Session.RevokedAt != nil || !now.Before(current.Session.LastSeenAt.Add(policy.IdleTimeout)) ||
		!now.Before(current.Session.AbsoluteExpiresAt) || (!currentToken && !previousToken) {
		return security.ErrConflict
	}
	if replacement.UserID != current.Session.UserID || !replacement.CreatedAt.Equal(current.Session.CreatedAt) ||
		!replacement.AbsoluteExpiresAt.Equal(current.Session.AbsoluteExpiresAt) || replacement.LastSeenAt.Before(current.Session.LastSeenAt) ||
		replacement.AuthVersion != expectedAccountVersion {
		return security.ErrInvalidInput
	}

	newRevision, err := checkedInt64(expectedRevision + 1)
	if err != nil {
		return err
	}
	newAuthVersion, err := checkedInt64(replacement.AuthVersion)
	if err != nil {
		return err
	}
	newGeneration, err := checkedInt64(replacement.Generation)
	if err != nil {
		return err
	}
	newCSRFEpoch, err := checkedInt64(replacement.CSRFEpoch)
	if err != nil {
		return err
	}
	rows, err := queries.AdvanceSession(ctx, platformsqlc.AdvanceSessionParams{
		ID: string(replacement.ID), Revision: revision, AuthVersion: accountVersion,
		AuthVersion_2: newAuthVersion, Revision_2: newRevision, Generation: newGeneration,
		CsrfEpoch: newCSRFEpoch, LastSeenAt: timestamp(replacement.LastSeenAt), RotatedAt: timestamp(replacement.RotatedAt),
		CurrentDigest: digestBytes(replacement.CurrentDigest), PreviousDigest: optionalDigestBytes(replacement.PreviousDigest),
		PreviousValidUntil: timestamp(replacement.PreviousValidUntil), CurrentTokenSealed: replacement.CurrentTokenSealed.Reveal(),
		RevokedAt: func() pgtype.Timestamptz {
			if replacement.RevokedAt == nil {
				return pgtype.Timestamptz{}
			}
			return timestamp(*replacement.RevokedAt)
		}(),
		CurrentDigest_2: digestBytes(presented),
	})
	if err != nil {
		return repositoryError(err)
	}
	if rows != 1 {
		return security.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return repositoryError(err)
	}
	return nil
}

func (s *Store) Revoke(ctx context.Context, sessionID security.ID, now time.Time) error {
	if !security.ValidID(sessionID) || now.IsZero() {
		return security.ErrInvalidInput
	}
	_, err := s.queries.RevokeSessionByID(ctx, platformsqlc.RevokeSessionByIDParams{
		ID:        string(sessionID),
		RevokedAt: timestamp(now),
	})
	return repositoryError(err)
}
