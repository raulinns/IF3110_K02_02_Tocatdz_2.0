package security

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

type TokenSource interface {
	Generate() (Secret, error)
	GenerateID() (ID, error)
}

type CSRFTokenIssuer interface {
	Token(context.Context, Principal) (Secret, error)
}

type LoginThrottle interface {
	CheckAccount(context.Context, string) error
	CheckIP(context.Context, string) error
}

type CheckedLogout interface {
	LogoutWithCheck(context.Context, Secret, SessionCheck) error
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type AuthServiceConfig struct {
	Credentials CredentialRepository
	Sessions    SessionRepository
	Passwords   PasswordHasher
	Tokens      TokenSource
	Protector   TokenProtector
	CSRF        CSRFTokenIssuer
	Throttle    LoginThrottle
	Clock       Clock
	Policy      SessionPolicy
}

type AuthService struct {
	deps      AuthServiceConfig
	dummyHash Secret
}

var _ Authenticator = (*AuthService)(nil)
var _ CheckedLogout = (*AuthService)(nil)

func missingDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func NewAuthService(ctx context.Context, cfg AuthServiceConfig) (*AuthService, error) {
	for _, dependency := range []any{cfg.Credentials, cfg.Sessions, cfg.Passwords, cfg.Tokens, cfg.Protector, cfg.CSRF, cfg.Throttle, cfg.Clock} {
		if missingDependency(dependency) {
			return nil, ErrInvalidInput
		}
	}
	if err := cfg.Policy.Validate(); err != nil {
		return nil, err
	}
	dummyPassword, err := cfg.Tokens.Generate()
	if err != nil {
		return nil, backendError(err)
	}
	dummyHash, err := cfg.Passwords.Hash(ctx, dummyPassword)
	if err != nil {
		return nil, backendError(err)
	}
	if _, err := parsePasswordPHC(dummyHash); err != nil {
		return nil, ErrInvalidInput
	}
	return &AuthService{deps: cfg, dummyHash: dummyHash}, nil
}

func NormalizeUsername(username string) (string, error) {
	if len(username) > 128 {
		return "", ErrInvalidInput
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if len(username) < 3 || len(username) > 32 {
		return "", ErrInvalidInput
	}
	for _, char := range username {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_') {
			return "", ErrInvalidInput
		}
	}
	return username, nil
}

func backendError(err error) error {
	for _, category := range []error{context.Canceled, context.DeadlineExceeded, ErrConflict, ErrUnavailable} {
		if errors.Is(err, category) {
			return category
		}
	}
	return ErrUnavailable
}

func (s *AuthService) rejectCredentials(ctx context.Context, password Secret) (SessionResult, error) {
	_, err := s.deps.Passwords.Verify(ctx, s.dummyHash, password)
	if err != nil && !errors.Is(err, ErrInvalidCredentials) {
		return SessionResult{}, backendError(err)
	}
	return SessionResult{}, ErrInvalidCredentials
}

func (s *AuthService) Login(ctx context.Context, input Credentials, previousCookie Secret) (SessionResult, error) {
	if err := ctx.Err(); err != nil {
		return SessionResult{}, err
	}
	username, err := NormalizeUsername(input.Username)
	if err != nil || len(input.Password.Reveal()) > MaxPasswordBytes || !utf8.ValidString(input.Password.Reveal()) {
		return SessionResult{}, ErrInvalidCredentials
	}
	if err := s.deps.Throttle.CheckAccount(ctx, username); err != nil {
		if errors.Is(err, ErrRateLimited) {
			return SessionResult{}, ErrRateLimited
		}
		return SessionResult{}, backendError(err)
	}
	record, err := s.deps.Credentials.FindByUsername(ctx, username)
	if errors.Is(err, ErrNotFound) {
		return s.rejectCredentials(ctx, input.Password)
	}
	if err != nil {
		return SessionResult{}, backendError(err)
	}
	if !record.Account.Active {
		return s.rejectCredentials(ctx, input.Password)
	}
	if _, err := parsePasswordPHC(record.PasswordHash); err != nil {
		return s.rejectCredentials(ctx, input.Password)
	}
	rehash, err := s.deps.Passwords.Verify(ctx, record.PasswordHash, input.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		return SessionResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return SessionResult{}, backendError(err)
	}
	account := record.Account
	if !ValidID(account.ID) || account.AuthVersion == 0 || account.Username != username {
		return SessionResult{}, ErrUnavailable
	}
	if rehash {
		replacement, err := s.deps.Passwords.Hash(ctx, input.Password)
		if err != nil {
			return SessionResult{}, backendError(err)
		}
		if err := s.deps.Credentials.Rehash(ctx, account.ID, record.PasswordHash, replacement, account.AuthVersion); err != nil {
			return SessionResult{}, backendError(err)
		}
	}
	id, err := s.deps.Tokens.GenerateID()
	if err != nil || !ValidID(id) {
		return SessionResult{}, ErrUnavailable
	}
	token, err := s.deps.Tokens.Generate()
	if err != nil {
		return SessionResult{}, backendError(err)
	}
	digest, err := DigestToken(token)
	if err != nil {
		return SessionResult{}, ErrUnavailable
	}
	if previous, err := DigestToken(previousCookie); err == nil && previous == digest {
		return SessionResult{}, ErrUnavailable
	}
	sealed, err := s.deps.Protector.Seal(ctx, token, id, 1)
	if err != nil {
		return SessionResult{}, backendError(err)
	}
	now := s.deps.Clock.Now().UTC()
	session := Session{
		ID: id, UserID: account.ID, AuthVersion: account.AuthVersion,
		Revision: 1, Generation: 1, CSRFEpoch: 1, CreatedAt: now,
		LastSeenAt: now, RotatedAt: now, AbsoluteExpiresAt: now.Add(s.deps.Policy.AbsoluteLifetime),
		CurrentDigest: digest, CurrentTokenSealed: sealed,
	}
	result, err := s.result(ctx, session, account, token)
	if err != nil {
		return SessionResult{}, err
	}
	var previous *TokenDigest
	if old, err := DigestToken(previousCookie); err == nil {
		previous = &old
	}
	if err := s.deps.Sessions.Create(ctx, session, account.AuthVersion, previous, s.deps.Policy); err != nil {
		return SessionResult{}, backendError(err)
	}
	return result, nil
}

func validSession(snapshot SessionSnapshot, now time.Time, policy SessionPolicy) bool {
	session, account := snapshot.Session, snapshot.Account
	if !ValidID(session.ID) || !ValidID(session.UserID) || account.ID != session.UserID || !account.Active ||
		session.AuthVersion == 0 || account.AuthVersion < session.AuthVersion || session.Revision == 0 ||
		session.Generation == 0 || session.CSRFEpoch == 0 || session.RevokedAt != nil || session.CurrentTokenSealed.Empty() {
		return false
	}
	if session.CreatedAt.IsZero() || session.CreatedAt.After(now) || session.LastSeenAt.Before(session.CreatedAt) ||
		session.RotatedAt.Before(session.CreatedAt) || session.RotatedAt.After(now) ||
		!session.AbsoluteExpiresAt.After(session.CreatedAt) || session.AbsoluteExpiresAt.After(session.CreatedAt.Add(policy.AbsoluteLifetime)) ||
		!now.Before(session.LastSeenAt.Add(policy.IdleTimeout)) || !now.Before(session.AbsoluteExpiresAt) {
		return false
	}
	if session.PreviousDigest == nil {
		return session.PreviousValidUntil.IsZero()
	}
	return *session.PreviousDigest != session.CurrentDigest && session.PreviousValidUntil.After(session.RotatedAt) &&
		!session.PreviousValidUntil.After(session.RotatedAt.Add(policy.GracePeriod)) && !session.PreviousValidUntil.After(session.AbsoluteExpiresAt)
}

func tokenMatches(session Session, digest TokenDigest, now time.Time) (previous, valid bool) {
	if digest == session.CurrentDigest {
		return false, true
	}
	if session.PreviousDigest != nil && digest == *session.PreviousDigest && now.Before(session.PreviousValidUntil) {
		return true, true
	}
	return false, false
}

func principalFor(session Session) Principal {
	return Principal{UserID: session.UserID, SessionID: session.ID, AuthVersion: session.AuthVersion, CSRFEpoch: session.CSRFEpoch}
}

func (s *AuthService) result(ctx context.Context, session Session, account Account, token Secret) (SessionResult, error) {
	principal := principalFor(session)
	csrf, err := s.deps.CSRF.Token(ctx, principal)
	if err != nil {
		return SessionResult{}, backendError(err)
	}
	return SessionResult{
		Principal: principal, Identity: Identity{UserID: account.ID, Username: account.Username, PlatformAdmin: account.PlatformAdmin},
		CookieToken: token, CSRFToken: csrf, ExpiresAt: session.AbsoluteExpiresAt,
	}, nil
}

func (s *AuthService) Authenticate(ctx context.Context, cookie Secret, check SessionCheck) (SessionResult, error) {
	digest, err := DigestToken(cookie)
	if err != nil {
		return SessionResult{}, ErrUnauthenticated
	}
	for range 3 {
		if err := ctx.Err(); err != nil {
			return SessionResult{}, err
		}
		snapshot, err := s.deps.Sessions.Lookup(ctx, digest)
		if errors.Is(err, ErrNotFound) {
			return SessionResult{}, ErrUnauthenticated
		}
		if err != nil {
			return SessionResult{}, backendError(err)
		}
		now := s.deps.Clock.Now().UTC()
		if !validSession(snapshot, now, s.deps.Policy) {
			return SessionResult{}, ErrUnauthenticated
		}
		previous, valid := tokenMatches(snapshot.Session, digest, now)
		if !valid || previous != snapshot.PresentedPrevious {
			return SessionResult{}, ErrUnauthenticated
		}
		replacement := snapshot.Session
		elevated := snapshot.Account.AuthVersion > replacement.AuthVersion
		if elevated {
			if replacement.CSRFEpoch == ^uint64(0) {
				return SessionResult{}, ErrUnauthenticated
			}
			replacement.AuthVersion = snapshot.Account.AuthVersion
			replacement.CSRFEpoch++
		}
		if check != nil {
			if err := check(ctx, principalFor(replacement)); err != nil {
				return SessionResult{}, err
			}
		}
		var token Secret
		if elevated || !previous && !now.Before(replacement.RotatedAt.Add(s.deps.Policy.RotationInterval)) {
			if replacement.Generation == ^uint64(0) {
				return SessionResult{}, ErrUnauthenticated
			}
			token, err = s.deps.Tokens.Generate()
			if err != nil {
				return SessionResult{}, backendError(err)
			}
			newDigest, err := DigestToken(token)
			if err != nil || newDigest == replacement.CurrentDigest || replacement.PreviousDigest != nil && newDigest == *replacement.PreviousDigest {
				return SessionResult{}, ErrUnavailable
			}
			oldDigest := replacement.CurrentDigest
			replacement.PreviousDigest = &oldDigest
			replacement.PreviousValidUntil = minTime(now.Add(s.deps.Policy.GracePeriod), replacement.AbsoluteExpiresAt)
			replacement.CurrentDigest = newDigest
			replacement.Generation++
			replacement.RotatedAt = now
			replacement.CurrentTokenSealed, err = s.deps.Protector.Seal(ctx, token, replacement.ID, replacement.Generation)
		} else {
			token, err = s.deps.Protector.Open(ctx, replacement.CurrentTokenSealed, replacement.ID, replacement.Generation)
		}
		if err != nil {
			if errors.Is(err, ErrUnauthenticated) {
				return SessionResult{}, ErrUnauthenticated
			}
			return SessionResult{}, backendError(err)
		}
		if got, err := DigestToken(token); err != nil || got != replacement.CurrentDigest {
			return SessionResult{}, ErrUnauthenticated
		}
		if now.After(replacement.LastSeenAt) {
			replacement.LastSeenAt = now
		}
		result, err := s.result(ctx, replacement, snapshot.Account, token)
		if err != nil {
			return SessionResult{}, err
		}
		// Saat konflik, baca ulang token awal agar rotasi atau logout dari request lain tetap dihormati.
		err = s.deps.Sessions.Advance(ctx, digest, replacement, snapshot.Session.Revision, snapshot.Account.AuthVersion, s.deps.Policy)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if errors.Is(err, ErrNotFound) {
			return SessionResult{}, ErrUnauthenticated
		}
		if err != nil {
			return SessionResult{}, backendError(err)
		}
		return result, nil
	}
	return SessionResult{}, ErrConflict
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *AuthService) Logout(ctx context.Context, cookie Secret) error {
	return s.LogoutWithCheck(ctx, cookie, nil)
}

func (s *AuthService) LogoutWithCheck(ctx context.Context, cookie Secret, check SessionCheck) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := DigestToken(cookie)
	if err != nil {
		return nil
	}
	snapshot, err := s.deps.Sessions.Lookup(ctx, digest)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return backendError(err)
	}
	if !ValidID(snapshot.Session.ID) {
		return ErrUnauthenticated
	}
	if digest != snapshot.Session.CurrentDigest && (snapshot.Session.PreviousDigest == nil || digest != *snapshot.Session.PreviousDigest) {
		return ErrUnauthenticated
	}
	now := s.deps.Clock.Now().UTC()
	if validSession(snapshot, now, s.deps.Policy) {
		_, matched := tokenMatches(snapshot.Session, digest, now)
		if matched && check != nil {
			candidate := snapshot.Session
			if snapshot.Account.AuthVersion > candidate.AuthVersion {
				if candidate.CSRFEpoch == ^uint64(0) {
					return ErrUnauthenticated
				}
				candidate.AuthVersion = snapshot.Account.AuthVersion
				candidate.CSRFEpoch++
			}
			if err := check(ctx, principalFor(candidate)); err != nil {
				return err
			}
		}
	}
	if err := s.deps.Sessions.Revoke(ctx, snapshot.Session.ID, now); err != nil {
		return backendError(err)
	}
	return nil
}
