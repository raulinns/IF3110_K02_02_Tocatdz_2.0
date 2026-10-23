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

type TokenDigest [32]byte

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
	CurrentTokenSealed Secret // Token disimpan terenkripsi supaya cookie terbaru bisa dikirim ulang selama grace period.
	RevokedAt          *time.Time
}

type SessionSnapshot struct {
	Session           Session
	Account           Account
	PresentedPrevious bool
}

type SessionRepository interface {
	// Buat session dan nonaktifkan session lama dalam satu transaksi, dengan grace period terbatas untuk user yang sama.
	Create(ctx context.Context, session Session, expectedAuthVersion uint64, previous *TokenDigest, policy SessionPolicy) error
	Lookup(ctx context.Context, digest TokenDigest) (SessionSnapshot, error)
	FindByID(ctx context.Context, sessionID ID) (SessionSnapshot, error)
	// Cek ulang revisi, akun, token, dan batas waktu setelah lock agar session yang dicabut tidak aktif lagi.
	Advance(ctx context.Context, presented TokenDigest, replacement Session, expectedRevision, expectedAccountVersion uint64, policy SessionPolicy) error
	Revoke(ctx context.Context, sessionID ID, now time.Time) error
}

type Clock interface{ Now() time.Time }

type TokenProtector interface {
	Seal(ctx context.Context, token Secret, sessionID ID, generation uint64) (Secret, error)
	Open(ctx context.Context, sealed Secret, sessionID ID, generation uint64) (Secret, error)
}

type Credentials struct {
	Username string
	Password Secret
}

type SessionResult struct {
	Principal   Principal
	Identity    Identity
	CookieToken Secret
	CSRFToken   Secret
	ExpiresAt   time.Time
}

// Cek CSRF sebelum setiap percobaan Advance
type SessionCheck func(context.Context, Principal) error

type Authenticator interface {
	Login(ctx context.Context, credentials Credentials, previousCookie Secret) (SessionResult, error)
	Authenticate(ctx context.Context, cookie Secret, beforeAdvance SessionCheck) (SessionResult, error)
	Logout(ctx context.Context, cookie Secret) error
}
