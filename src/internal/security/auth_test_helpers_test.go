package security

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type authTestClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *authTestClock) Now() time.Time      { c.mu.RLock(); defer c.mu.RUnlock(); return c.now }
func (c *authTestClock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type authTestStore struct {
	mu                                                                           sync.Mutex
	clock                                                                        Clock
	accounts                                                                     map[ID]CredentialRecord
	sessions                                                                     map[ID]Session
	grants                                                                       map[ID][]DatabaseAccess
	createCalls, advanceCalls, lookupCalls, grantCalls, rehashCalls              int
	lookupErr, createErr, advanceErr, revokeErr, accountErr, grantErr, rehashErr error
	createHook, advanceHook                                                      func(*authTestStore)
}

func copySession(session Session) Session {
	if session.PreviousDigest != nil {
		previous := *session.PreviousDigest
		session.PreviousDigest = &previous
	}
	if session.RevokedAt != nil {
		revoked := *session.RevokedAt
		session.RevokedAt = &revoked
	}
	return session
}

func (s *authTestStore) FindByUsername(ctx context.Context, username string) (CredentialRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return CredentialRecord{}, err
	}
	if s.accountErr != nil {
		return CredentialRecord{}, s.accountErr
	}
	for _, record := range s.accounts {
		if record.Account.Username == username {
			return record, nil
		}
	}
	return CredentialRecord{}, ErrNotFound
}

func (s *authTestStore) FindAccount(ctx context.Context, id ID) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	record, ok := s.accounts[id]
	if !ok {
		return Account{}, ErrNotFound
	}
	return record.Account, nil
}

func (s *authTestStore) Rehash(ctx context.Context, id ID, expected, replacement Secret, version uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rehashCalls++
	if s.rehashErr != nil {
		return s.rehashErr
	}
	record := s.accounts[id]
	if record.PasswordHash.Reveal() != expected.Reveal() || record.Account.AuthVersion != version {
		return ErrConflict
	}
	record.PasswordHash = replacement
	s.accounts[id] = record
	return nil
}

func (s *authTestStore) findDigest(digest TokenDigest) (Session, bool, bool) {
	for _, session := range s.sessions {
		if session.CurrentDigest == digest {
			return copySession(session), false, true
		}
		if session.PreviousDigest != nil && *session.PreviousDigest == digest {
			return copySession(session), true, true
		}
	}
	return Session{}, false, false
}

func (s *authTestStore) Create(ctx context.Context, session Session, version uint64, previous *TokenDigest, policy SessionPolicy) error {
	s.mu.Lock()
	hook := s.createHook
	s.createHook = nil
	s.mu.Unlock()
	if hook != nil {
		hook(s)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.createErr != nil {
		return s.createErr
	}
	account := s.accounts[session.UserID].Account
	if !account.Active || account.AuthVersion != version {
		return ErrConflict
	}
	if _, exists := s.sessions[session.ID]; exists {
		return ErrConflict
	}
	now := s.clock.Now()
	if previous != nil {
		old, wasPrevious, exists := s.findDigest(*previous)
		if exists {
			oldAccount := s.accounts[old.UserID].Account
			eligible := old.RevokedAt == nil && old.UserID == session.UserID && oldAccount.Active && old.AuthVersion == version &&
				now.Before(old.AbsoluteExpiresAt) && now.Before(old.LastSeenAt.Add(policy.IdleTimeout))
			deadline := minTime(now.Add(policy.GracePeriod), minTime(old.AbsoluteExpiresAt, old.LastSeenAt.Add(policy.IdleTimeout)))
			if wasPrevious {
				deadline = minTime(deadline, old.PreviousValidUntil)
			}
			if eligible && now.Before(deadline) {
				value := *previous
				session.PreviousDigest = &value
				session.PreviousValidUntil = deadline
				if wasPrevious {
					old.PreviousDigest = nil
					old.PreviousValidUntil = time.Time{}
				} else {
					old.CurrentDigest = TokenDigest{}
				}
			}
			old.RevokedAt = &now
			old.Revision++
			s.sessions[old.ID] = old
		}
	}
	s.sessions[session.ID] = copySession(session)
	return nil
}

func (s *authTestStore) Lookup(ctx context.Context, digest TokenDigest) (SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookupCalls++
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}
	if s.lookupErr != nil {
		return SessionSnapshot{}, s.lookupErr
	}
	session, previous, exists := s.findDigest(digest)
	if !exists {
		return SessionSnapshot{}, ErrNotFound
	}
	return SessionSnapshot{Session: session, Account: s.accounts[session.UserID].Account, PresentedPrevious: previous}, nil
}

func (s *authTestStore) FindByID(ctx context.Context, id ID) (SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}
	if s.lookupErr != nil {
		return SessionSnapshot{}, s.lookupErr
	}
	session, exists := s.sessions[id]
	if !exists {
		return SessionSnapshot{}, ErrNotFound
	}
	return SessionSnapshot{Session: copySession(session), Account: s.accounts[session.UserID].Account}, nil
}

func (s *authTestStore) Advance(ctx context.Context, digest TokenDigest, replacement Session, revision, accountVersion uint64, policy SessionPolicy) error {
	s.mu.Lock()
	hook := s.advanceHook
	s.advanceHook = nil
	s.mu.Unlock()
	if hook != nil {
		hook(s)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.advanceErr != nil {
		return s.advanceErr
	}
	current, exists := s.sessions[replacement.ID]
	account := s.accounts[current.UserID].Account
	now := s.clock.Now()
	if !exists || current.Revision != revision || !account.Active || account.AuthVersion != accountVersion || current.RevokedAt != nil ||
		!now.Before(current.LastSeenAt.Add(policy.IdleTimeout)) || !now.Before(current.AbsoluteExpiresAt) {
		return ErrConflict
	}
	currentToken := current.CurrentDigest == digest
	previousToken := current.PreviousDigest != nil && *current.PreviousDigest == digest && now.Before(current.PreviousValidUntil)
	if !currentToken && !previousToken {
		return ErrConflict
	}
	if replacement.UserID != current.UserID || replacement.CreatedAt != current.CreatedAt || replacement.AbsoluteExpiresAt != current.AbsoluteExpiresAt ||
		replacement.LastSeenAt.Before(current.LastSeenAt) || replacement.AuthVersion != accountVersion {
		return ErrInvalidInput
	}
	replacement.Revision = current.Revision + 1
	s.sessions[replacement.ID] = copySession(replacement)
	return nil
}

func (s *authTestStore) Revoke(ctx context.Context, id ID, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.revokeErr != nil {
		return s.revokeErr
	}
	if session, exists := s.sessions[id]; exists && session.RevokedAt == nil {
		session.RevokedAt = &now
		session.Revision++
		s.sessions[id] = session
	}
	return nil
}

func (s *authTestStore) AccessForDatabase(ctx context.Context, user, database ID) (AccessSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grantCalls++
	if err := ctx.Err(); err != nil {
		return AccessSnapshot{}, err
	}
	if s.grantErr != nil {
		return AccessSnapshot{}, s.grantErr
	}
	return AccessSnapshot{Account: s.accounts[user].Account, GroupAccess: append([]DatabaseAccess(nil), s.grants[database]...)}, nil
}

type authFixture struct {
	service  *AuthService
	store    *authTestStore
	clock    *authTestClock
	guard    *HMACCSRFGuard
	cookies  *SessionCookies
	throttle *LoginLimiter
	http     *HTTPMiddleware
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	clock := &authTestClock{now: time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)}
	hasher, _ := NewArgon2idHasher(stubKey)
	hash, err := hasher.Hash(context.Background(), NewSecret("a valid test password"))
	if err != nil {
		t.Fatal(err)
	}
	store := &authTestStore{clock: clock, accounts: map[ID]CredentialRecord{
		testUserID: {Account: Account{ID: testUserID, Username: "alice", Active: true, AuthVersion: 1}, PasswordHash: hash},
	}, sessions: make(map[ID]Session), grants: make(map[ID][]DatabaseAccess)}
	protector, _ := NewAESGCMTokenProtector(testKey(1))
	guard, _ := NewHMACCSRFGuard("https://platform.example", testKey(2))
	throttle, _ := NewLoginLimiter(clock)
	service, err := NewAuthService(context.Background(), AuthServiceConfig{
		Credentials: store, Sessions: store, Passwords: hasher, Tokens: NewTokenGenerator(), Protector: protector,
		CSRF: guard, Throttle: throttle, Clock: clock, Policy: DefaultSessionPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cookies, _ := NewSessionCookies("https://platform.example", true)
	guardHTTP, err := NewHTTPMiddleware(HTTPMiddlewareConfig{Auth: service, Logout: service, CSRF: guard, Cookies: cookies, Throttle: throttle})
	if err != nil {
		t.Fatal(err)
	}
	return &authFixture{service: service, store: store, clock: clock, guard: guard, cookies: cookies, throttle: throttle, http: guardHTTP}
}

func (f *authFixture) login(t *testing.T) SessionResult {
	t.Helper()
	result, err := f.service.Login(context.Background(), Credentials{Username: " ALIce ", Password: NewSecret("a valid test password")}, Secret{})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertAuthError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("got error %v; want %v", got, want)
	}
}
