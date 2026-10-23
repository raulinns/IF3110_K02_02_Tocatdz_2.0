package security

import (
	"context"
	"time"
)

const (
	SessionTokenBytes = 32
	SessionCookieName = "tocatdz_session"
	CSRFFieldName     = "csrf_token"
	CSRFHeaderName    = "X-CSRF-Token"
)

// SessionPolicy values are team decisions, not durations imposed by the PDF.
// Clock.Now is UTC; a deadline equal to Now is already expired.
type SessionPolicy struct {
	IdleTimeout      time.Duration
	AbsoluteLifetime time.Duration
	RotationInterval time.Duration
	GracePeriod      time.Duration
}

func DefaultSessionPolicy() SessionPolicy {
	return SessionPolicy{
		IdleTimeout: 30 * time.Minute, AbsoluteLifetime: 12 * time.Hour,
		RotationInterval: 15 * time.Minute, GracePeriod: 30 * time.Second,
	}
}

func (p SessionPolicy) Validate() error {
	if p.GracePeriod <= 0 || p.GracePeriod >= p.RotationInterval ||
		p.RotationInterval >= p.IdleTimeout || p.IdleTimeout >= p.AbsoluteLifetime {
		return ErrInvalidInput
	}
	return nil
}

// TokenDigest contains SHA-256(raw decoded 32-byte random token); tokens are
// transported as unpadded base64url. Digests are never logged or returned to UI.
type TokenDigest [32]byte

// Session is one logical family across token rotations. ID is not the cookie.
// CurrentTokenSealed is AEAD ciphertext of the current raw token, allowing a
// grace request to receive the SAME current cookie after a lost response.
// AAD binds the ciphertext to session ID and generation. No plaintext token is
// persisted. Session repository implementations must not decrypt this field.
type Session struct {
	ID                 ID
	UserID             ID
	AuthVersion        uint64
	Revision           uint64
	Generation         uint64
	CSRFEpoch          uint64
	CreatedAt          time.Time
	LastSeenAt         time.Time
	AbsoluteExpiresAt  time.Time
	RotatedAt          time.Time
	CurrentDigest      TokenDigest
	PreviousDigest     *TokenDigest
	PreviousValidUntil time.Time
	CurrentTokenSealed Secret
	RevokedAt          *time.Time
}

// SessionSnapshot is a consistent read of session + CURRENT account state.
// Account privilege data must not come from a cached role on the session.
type SessionSnapshot struct {
	Session           Session
	Account           Account
	PresentedPrevious bool
}

// SessionRepository owns persistence mechanics, not cookie parsing or policy.
// Every mutation is atomic. See the document for row locks/CAS, grace behavior,
// and the account security-mutation transaction required of admin adapters.
type SessionRepository interface {
	// Create checks active account + expectedAuthVersion in the same transaction
	// as insertion; session.AuthVersion must equal expectedAuthVersion.
	// previous, if supplied, retires a prior browser family atomically. For a
	// still-valid same-user/same-account-epoch token, transfer that digest to the
	// new family's previous slot with bounded grace. An account switch never
	// transfers authentication. Failed login never revokes another user.
	Create(ctx context.Context, session Session, expectedAuthVersion uint64, previous *TokenDigest, policy SessionPolicy) error
	// Lookup recognizes only current or previous digests. Unknown -> ErrNotFound.
	// Service must still enforce all deadlines, revocation, and account state.
	Lookup(ctx context.Context, digest TokenDigest) (SessionSnapshot, error)
	// FindByID supports Authorizer and CSRF checks from middleware-established
	// Principal. The ID is internal, never accepted as bearer authentication.
	FindByID(ctx context.Context, sessionID ID) (SessionSnapshot, error)
	// Advance checks revision, active account, current account AuthVersion,
	// unrevoked state, presented digest/grace, and idle/absolute deadlines under
	// a lock. Read the injected clock again AFTER acquiring locks (not stale
	// request-start time). Authoritative DB adapters may use clock_timestamp().
	// It updates only mutable fields of replacement and increments Revision.
	// CreatedAt/UserID/ID/AbsoluteExpiresAt are immutable. Concurrent rotation,
	// logout, revocation, or privilege mutation -> ErrConflict; never resurrect.
	Advance(ctx context.Context, presented TokenDigest, replacement Session, expectedRevision, expectedAccountVersion uint64, policy SessionPolicy) error
	// Revocation is idempotent, covers current AND previous tokens, and is
	// committed before logout success. Unknown/already revoked is a no-op.
	Revoke(ctx context.Context, sessionID ID, now time.Time) error
}

type Clock interface{ Now() time.Time }

// TokenProtector seals current tokens using a shared persisted key; key ID,
// nonce, ciphertext, and tag may be encoded in Secret. Open authenticates AAD.
// Entropy/key failure is fatal to the operation, never a plaintext fallback.
type TokenProtector interface {
	Seal(ctx context.Context, token Secret, sessionID ID, generation uint64) (Secret, error)
	Open(ctx context.Context, sealed Secret, sessionID ID, generation uint64) (Secret, error)
}

type Credentials struct {
	Username string
	Password Secret
}

// SessionResult is transport-internal. Only Identity may be a public response.
// CookieToken goes solely into Set-Cookie. CSRFToken is a separate HMAC token
// bound to session family + CSRFEpoch; it may be exposed to same-origin forms.
type SessionResult struct {
	Principal   Principal
	Identity    Identity
	CookieToken Secret
	CSRFToken   Secret
	ExpiresAt   time.Time
}

// SessionCheck is a read-only check run on the candidate Principal BEFORE any
// idle touch/rotation is committed. Middleware supplies CSRF validation on unsafe
// methods; safe methods may pass nil. Re-run it after every CAS conflict/reload.
// No handler side effects or writes are permitted in this callback. On privilege
// elevation the candidate includes the new CSRFEpoch, so stale forms fail closed.
type SessionCheck func(context.Context, Principal) error

// Authenticator.Login receives previousCookie from the cookie only (if any).
// Authenticate validates/touches each protected request and returns the current
// cookie, including during grace recovery. No raw token in URL/body/JSON/log.
// Logout accepts expired tokens too so the backend family can still be revoked.
type Authenticator interface {
	Login(ctx context.Context, credentials Credentials, previousCookie Secret) (SessionResult, error)
	Authenticate(ctx context.Context, cookie Secret, beforeAdvance SessionCheck) (SessionResult, error)
	Logout(ctx context.Context, cookie Secret) error
}
