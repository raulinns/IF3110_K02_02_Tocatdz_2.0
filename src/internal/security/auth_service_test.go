package security

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuthLoginCreatesOpaqueSessionAndNormalizesUsername(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	stored := f.store.sessions[result.Principal.SessionID]
	if result.Identity.Username != "alice" || result.Principal.UserID != testUserID || result.Principal.AuthVersion != 1 ||
		stored.CurrentTokenSealed.Empty() || stored.Generation != 1 || stored.Revision != 1 || stored.CSRFEpoch != 1 ||
		stored.AbsoluteExpiresAt != f.clock.Now().Add(12*time.Hour) || stored.CurrentTokenSealed.Reveal() == result.CookieToken.Reveal() {
		t.Fatal("invalid session created")
	}
	if f.guard.VerifyToken(context.Background(), result.Principal, result.CSRFToken) != nil {
		t.Fatal("login CSRF binding invalid")
	}
}

func TestAuthLoginDenialsHaveNoSessionEffects(t *testing.T) {
	for _, scenario := range []string{"unknown", "inactive", "wrong password", "invalid username", "oversized password", "corrupt hash", "outage"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			input := Credentials{Username: "alice", Password: NewSecret("a valid test password")}
			want := ErrInvalidCredentials
			switch scenario {
			case "unknown":
				input.Username = "absent"
			case "inactive":
				record := f.store.accounts[testUserID]
				record.Account.Active = false
				f.store.accounts[testUserID] = record
			case "wrong password":
				input.Password = NewSecret("wrong password")
			case "invalid username":
				input.Username = "bad!"
			case "oversized password":
				input.Password = NewSecret(strings.Repeat("x", MaxPasswordBytes+1))
			case "corrupt hash":
				record := f.store.accounts[testUserID]
				record.PasswordHash = NewSecret("plaintext")
				f.store.accounts[testUserID] = record
			case "outage":
				f.store.accountErr = errors.New("private database credential")
				want = ErrUnavailable
			}
			result, err := f.service.Login(context.Background(), input, testToken(5))
			assertAuthError(t, err, want)
			if !result.CookieToken.Empty() || f.store.createCalls != 0 || len(f.store.sessions) != 0 {
				t.Fatal("failed login changed sessions")
			}
		})
	}
}

func TestAuthLoginRotationGraceAndAccountSwitch(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	f.clock.add(time.Second)
	second, err := f.service.Login(context.Background(), Credentials{Username: "alice", Password: NewSecret("a valid test password")}, first.CookieToken)
	if err != nil || first.Principal.SessionID == second.Principal.SessionID || first.CookieToken.Reveal() == second.CookieToken.Reveal() {
		t.Fatal("login did not replace session")
	}
	recovered, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	if err != nil || recovered.CookieToken.Reveal() != second.CookieToken.Reveal() {
		t.Fatal("login grace did not recover successor")
	}
	f.clock.add(30 * time.Second)
	_, err = f.service.Authenticate(context.Background(), first.CookieToken, nil)
	assertAuthError(t, err, ErrUnauthenticated)
	other := f.store.accounts[testUserID]
	other.Account.ID = otherID
	other.Account.Username = "bob"
	f.store.accounts[otherID] = other
	third, err := f.service.Login(context.Background(), Credentials{Username: "bob", Password: NewSecret("a valid test password")}, second.CookieToken)
	if err != nil || third.Principal.UserID != otherID {
		t.Fatal("account switch failed")
	}
	_, err = f.service.Authenticate(context.Background(), second.CookieToken, nil)
	assertAuthError(t, err, ErrUnauthenticated)
}

func TestAuthPeriodicRotationRecoversLostResponseWithoutExtendingAbsolute(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	f.clock.add(15 * time.Minute)
	second, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	if err != nil || second.CookieToken.Reveal() == first.CookieToken.Reveal() || second.ExpiresAt != first.ExpiresAt {
		t.Fatal("rotation failed or extended absolute expiry")
	}
	rotated := copySession(f.store.sessions[first.Principal.SessionID])
	f.clock.add(10 * time.Second)
	recovered, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	after := f.store.sessions[first.Principal.SessionID]
	if err != nil || recovered.CookieToken.Reveal() != second.CookieToken.Reveal() || after.Generation != rotated.Generation ||
		after.PreviousValidUntil != rotated.PreviousValidUntil || after.RotatedAt != rotated.RotatedAt || recovered.CSRFToken.Reveal() != first.CSRFToken.Reveal() {
		t.Fatal("grace rotated again or extended deadline/CSRF binding")
	}
	f.clock.add(20 * time.Second)
	_, err = f.service.Authenticate(context.Background(), first.CookieToken, nil)
	assertAuthError(t, err, ErrUnauthenticated)
}

func TestAuthIdleAbsoluteAndRevocationBoundaries(t *testing.T) {
	for _, scenario := range []string{"idle equality", "absolute equality", "revoked", "inactive", "epoch rollback", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			result := f.login(t)
			session := f.store.sessions[result.Principal.SessionID]
			record := f.store.accounts[testUserID]
			switch scenario {
			case "idle equality":
				f.clock.add(30 * time.Minute)
			case "absolute equality":
				f.clock.add(12 * time.Hour)
				session.LastSeenAt = f.clock.Now()
				f.store.sessions[session.ID] = session
			case "revoked":
				_ = f.store.Revoke(context.Background(), session.ID, f.clock.Now())
			case "inactive":
				record.Account.Active = false
				f.store.accounts[testUserID] = record
			case "epoch rollback":
				record.Account.AuthVersion = 0
				f.store.accounts[testUserID] = record
			case "malformed":
				session.Generation = 0
				f.store.sessions[session.ID] = session
			}
			_, err := f.service.Authenticate(context.Background(), result.CookieToken, nil)
			assertAuthError(t, err, ErrUnauthenticated)
			if f.store.advanceCalls != 0 {
				t.Fatal("invalid session refreshed")
			}
		})
	}
}

func TestAuthElevationChangesCSRFBeforeAnyMutation(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	record := f.store.accounts[testUserID]
	record.Account.AuthVersion++
	record.Account.PlatformAdmin = true
	f.store.accounts[testUserID] = record
	check := func(ctx context.Context, p Principal) error { return f.guard.VerifyToken(ctx, p, first.CSRFToken) }
	_, err := f.service.Authenticate(context.Background(), first.CookieToken, check)
	assertAuthError(t, err, ErrCSRF)
	if f.store.advanceCalls != 0 {
		t.Fatal("rejected elevation form mutated session")
	}
	second, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	if err != nil || second.Principal.AuthVersion != 2 || second.Principal.CSRFEpoch != 2 || second.CookieToken.Reveal() == first.CookieToken.Reveal() ||
		second.CSRFToken.Reveal() == first.CSRFToken.Reveal() || second.ExpiresAt != first.ExpiresAt || !second.Identity.PlatformAdmin {
		t.Fatal("elevation transition invalid")
	}
}

func TestAuthConflictRetryRechecksCSRFAndHonorsLogout(t *testing.T) {
	for _, scenario := range []string{"retry", "exhausted", "logout", "elevation"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			first := f.login(t)
			switch scenario {
			case "retry":
				f.store.advanceHook = func(store *authTestStore) {
					store.mu.Lock()
					defer store.mu.Unlock()
					current := store.sessions[first.Principal.SessionID]
					current.Revision++
					store.sessions[current.ID] = current
				}
			case "exhausted":
				f.store.advanceErr = ErrConflict
			case "logout":
				f.store.advanceHook = func(store *authTestStore) {
					_ = store.Revoke(context.Background(), first.Principal.SessionID, f.clock.Now())
				}
			case "elevation":
				f.store.advanceHook = func(store *authTestStore) {
					store.mu.Lock()
					defer store.mu.Unlock()
					record := store.accounts[testUserID]
					record.Account.AuthVersion++
					store.accounts[testUserID] = record
				}
			}
			checks := 0
			check := func(ctx context.Context, p Principal) error {
				checks++
				return f.guard.VerifyToken(ctx, p, first.CSRFToken)
			}
			_, err := f.service.Authenticate(context.Background(), first.CookieToken, check)
			switch scenario {
			case "retry":
				if err != nil || checks != 2 {
					t.Fatal("CAS retry skipped revalidation")
				}
			case "exhausted":
				assertAuthError(t, err, ErrConflict)
				if f.store.advanceCalls != 3 {
					t.Fatal("retry not bounded")
				}
			case "logout":
				assertAuthError(t, err, ErrUnauthenticated)
			case "elevation":
				assertAuthError(t, err, ErrCSRF)
				if checks != 2 {
					t.Fatal("changed epoch skipped CSRF validation")
				}
			}
		})
	}
}

func TestAuthConcurrentRotationHasOneSuccessor(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	f.clock.add(15 * time.Minute)
	var wg sync.WaitGroup
	results := make(chan SessionResult, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
			results <- result
			failures <- err
		}()
	}
	wg.Wait()
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	a, b := <-results, <-results
	if a.CookieToken.Reveal() != b.CookieToken.Reveal() || f.store.sessions[first.Principal.SessionID].Generation != 2 {
		t.Fatal("concurrent rotations produced multiple successors")
	}
}

func TestAuthLogoutChecksLiveCSRFAndRevokesExpiredSession(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	check := func(context.Context, Principal) error { return ErrCSRF }
	assertAuthError(t, f.service.LogoutWithCheck(context.Background(), result.CookieToken, check), ErrCSRF)
	if f.store.sessions[result.Principal.SessionID].RevokedAt != nil {
		t.Fatal("rejected logout revoked session")
	}
	f.clock.add(30 * time.Minute)
	if err := f.service.LogoutWithCheck(context.Background(), result.CookieToken, check); err != nil {
		t.Fatal(err)
	}
	if f.store.sessions[result.Principal.SessionID].RevokedAt == nil {
		t.Fatal("expired session not revoked")
	}
	if err := f.service.Logout(context.Background(), result.CookieToken); err != nil {
		t.Fatal("logout not idempotent")
	}
	if err := f.service.Logout(context.Background(), testToken(8)); err != nil {
		t.Fatal("unknown token not idempotent")
	}
}

func TestAuthStorageAndCryptoFailuresDoNotReturnSession(t *testing.T) {
	f := newAuthFixture(t)
	f.store.createHook = func(store *authTestStore) {
		store.mu.Lock()
		defer store.mu.Unlock()
		record := store.accounts[testUserID]
		record.Account.AuthVersion++
		store.accounts[testUserID] = record
	}
	result, err := f.service.Login(context.Background(), Credentials{Username: "alice", Password: NewSecret("a valid test password")}, Secret{})
	assertAuthError(t, err, ErrConflict)
	if !result.CookieToken.Empty() || len(f.store.sessions) != 0 {
		t.Fatal("password reset race issued a session")
	}
	f = newAuthFixture(t)
	first := f.login(t)
	protector, _ := NewAESGCMTokenProtector(testKey(9))
	f.service.deps.Protector = protector
	_, err = f.service.Authenticate(context.Background(), first.CookieToken, nil)
	assertAuthError(t, err, ErrUnauthenticated)
	if f.store.advanceCalls != 0 {
		t.Fatal("decryption failure refreshed session")
	}
	f.store.revokeErr = errors.New("private database password")
	assertAuthError(t, f.service.Logout(context.Background(), first.CookieToken), ErrUnavailable)
}

func TestAuthConstructorAndUsernameValidation(t *testing.T) {
	if _, err := NewAuthService(context.Background(), AuthServiceConfig{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing dependencies accepted")
	}
	f := newAuthFixture(t)
	cfg := f.service.deps
	var nilStore *authTestStore
	cfg.Sessions = nilStore
	if _, err := NewAuthService(context.Background(), cfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("typed nil repository accepted")
	}
	cfg = f.service.deps
	cfg.Policy = SessionPolicy{}
	if _, err := NewAuthService(context.Background(), cfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("invalid policy accepted")
	}
	for _, input := range []string{"ab", "a!b", "Älice", strings.Repeat("a", 129)} {
		if _, err := NormalizeUsername(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid username accepted")
		}
	}
}

type rehashTestHasher struct{ PasswordHasher }

func (h rehashTestHasher) Verify(ctx context.Context, hash, password Secret) (bool, error) {
	_, err := h.PasswordHasher.Verify(ctx, hash, password)
	return err == nil, err
}

func TestAuthRehashUsesCASAndNeverIssuesSessionOnConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		f := newAuthFixture(t)
		cfg := f.service.deps
		cfg.Passwords = rehashTestHasher{cfg.Passwords}
		service, err := NewAuthService(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if conflict {
			f.store.rehashErr = ErrConflict
		}
		result, err := service.Login(context.Background(), Credentials{Username: "alice", Password: NewSecret("a valid test password")}, Secret{})
		if f.store.rehashCalls != 1 {
			t.Fatal("successful old hash was not rehashed")
		}
		if conflict {
			assertAuthError(t, err, ErrConflict)
			if !result.CookieToken.Empty() || f.store.createCalls != 0 {
				t.Fatal("rehash conflict issued session")
			}
		} else if err != nil || result.Principal.AuthVersion != 1 {
			t.Fatal("representation rehash changed security epoch")
		}
	}
}

type verificationSpy struct {
	PasswordHasher
	seen []Secret
}

func (s *verificationSpy) Verify(ctx context.Context, encoded, password Secret) (bool, error) {
	s.seen = append(s.seen, encoded)
	return s.PasswordHasher.Verify(ctx, encoded, password)
}

func TestAuthUnknownAndInactiveAccountsUseDummyVerification(t *testing.T) {
	for _, inactive := range []bool{false, true} {
		f := newAuthFixture(t)
		cfg := f.service.deps
		spy := &verificationSpy{PasswordHasher: cfg.Passwords}
		cfg.Passwords = spy
		service, err := NewAuthService(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		username := "absent"
		if inactive {
			record := f.store.accounts[testUserID]
			record.Account.Active = false
			f.store.accounts[testUserID] = record
			username = "alice"
		}
		_, err = service.Login(context.Background(), Credentials{Username: username, Password: NewSecret("wrong")}, Secret{})
		assertAuthError(t, err, ErrInvalidCredentials)
		if len(spy.seen) != 1 || spy.seen[0].Reveal() != service.dummyHash.Reveal() {
			t.Fatal("missing or inactive account skipped dummy KDF")
		}
	}
}

func TestAuthContinuousActivityNeverExtendsAbsoluteLifetime(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	current := first.CookieToken
	for range 47 {
		f.clock.add(15 * time.Minute)
		result, err := f.service.Authenticate(context.Background(), current, nil)
		if err != nil || result.ExpiresAt != first.ExpiresAt {
			t.Fatal("active rotation extended absolute expiry")
		}
		current = result.CookieToken
	}
	f.clock.add(15 * time.Minute)
	_, err := f.service.Authenticate(context.Background(), current, nil)
	assertAuthError(t, err, ErrUnauthenticated)
}

func TestAuthContextAndMalformedTokenDoNotReachPersistence(t *testing.T) {
	f := newAuthFixture(t)
	_, err := f.service.Authenticate(context.Background(), NewSecret("malformed"), nil)
	assertAuthError(t, err, ErrUnauthenticated)
	if f.store.lookupCalls != 0 {
		t.Fatal("malformed token reached repository")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.service.Login(ctx, Credentials{Username: "alice", Password: NewSecret("a valid test password")}, Secret{})
	assertAuthError(t, err, context.Canceled)
	_, err = f.service.Authenticate(ctx, testToken(1), nil)
	assertAuthError(t, err, context.Canceled)
	assertAuthError(t, f.service.Logout(ctx, testToken(1)), context.Canceled)
	if f.store.lookupCalls != 0 || f.store.createCalls != 0 {
		t.Fatal("canceled operation reached persistence")
	}
}

func TestAuthExpiryWhileWaitingForLockCannotRefreshSession(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	f.store.advanceHook = func(*authTestStore) { f.clock.add(30 * time.Minute) }
	_, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	assertAuthError(t, err, ErrUnauthenticated)
	if f.store.sessions[first.Principal.SessionID].LastSeenAt != f.store.sessions[first.Principal.SessionID].CreatedAt {
		t.Fatal("expired request refreshed session after waiting for lock")
	}
}
