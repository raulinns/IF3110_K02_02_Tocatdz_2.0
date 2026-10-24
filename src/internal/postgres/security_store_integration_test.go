package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const testPassword = "password integrasi yang panjang"

type authFixture struct {
	store   *Store
	clock   *testClock
	service *security.AuthService
	user    security.ID
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	pool := testPlatform(t)
	clock := newTestClock()
	store, err := NewStore(pool, clock)
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := security.NewArgon2idHasher(func(password, salt []byte, _, _ uint32, _ uint8, keyLen uint32) []byte {
		out := make([]byte, keyLen)
		copy(out, append(append([]byte{}, password...), salt...))
		return out
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(context.Background(), security.NewSecret(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	user := newID(t)
	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ($1, 'budi', $2)", string(user), hash.Reveal())
	protector, _ := security.NewAESGCMTokenProtector(testKey(1))
	csrf, _ := security.NewHMACCSRFGuard("http://localhost:8080", testKey(2))
	limiter, _ := security.NewLoginLimiter(clock)
	service, err := security.NewAuthService(context.Background(), security.AuthServiceConfig{
		Credentials: store, Sessions: store, Passwords: hasher, Tokens: security.NewTokenGenerator(), Protector: protector,
		CSRF: csrf, Throttle: limiter, Clock: clock, Policy: security.DefaultSessionPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &authFixture{store: store, clock: clock, service: service, user: user}
}

func (f *authFixture) login(t *testing.T, previous security.Secret) security.SessionResult {
	t.Helper()
	result, err := f.service.Login(context.Background(), security.Credentials{Username: " BUDI ", Password: security.NewSecret(testPassword)}, previous)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testPlatform(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal("second migrate failed")
	}
}

func TestSessionLifecycleAgainstPostgreSQL(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	if _, err := f.service.Login(ctx, security.Credentials{Username: "budi", Password: security.NewSecret("password salah yang panjang")}, security.Secret{}); !errors.Is(err, security.ErrInvalidCredentials) {
		t.Fatal("wrong password accepted")
	}
	first := f.login(t, security.Secret{})
	if _, err := f.service.Authenticate(ctx, first.CookieToken, nil); err != nil {
		t.Fatal("fresh session rejected")
	}
	f.clock.add(15 * time.Minute)
	rotated, err := f.service.Authenticate(ctx, first.CookieToken, nil)
	if err != nil || rotated.CookieToken.Reveal() == first.CookieToken.Reveal() {
		t.Fatal("periodic rotation did not issue a new token")
	}
	recovered, err := f.service.Authenticate(ctx, first.CookieToken, nil)
	if err != nil || recovered.CookieToken.Reveal() != rotated.CookieToken.Reveal() {
		t.Fatal("previous token not recovered during grace")
	}
	f.clock.add(31 * time.Second)
	if _, err := f.service.Authenticate(ctx, first.CookieToken, nil); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("previous token accepted after grace")
	}
	if _, err := f.service.Authenticate(ctx, rotated.CookieToken, nil); err != nil {
		t.Fatal("current token rejected after grace")
	}
	f.clock.add(31 * time.Minute)
	if _, err := f.service.Authenticate(ctx, rotated.CookieToken, nil); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("idle session accepted")
	}
}

func TestLoginTransfersPreviousCookieAndLogoutRevokes(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	first := f.login(t, security.Secret{})
	second := f.login(t, first.CookieToken)
	if second.Principal.SessionID == first.Principal.SessionID {
		t.Fatal("login reused the previous session family")
	}
	recovered, err := f.service.Authenticate(ctx, first.CookieToken, nil)
	if err != nil || recovered.Principal.SessionID != second.Principal.SessionID {
		t.Fatal("previous cookie not transferred into the new family during grace")
	}
	if err := f.service.Logout(ctx, second.CookieToken); err != nil {
		t.Fatal(err)
	}
	for _, token := range []security.Secret{first.CookieToken, second.CookieToken} {
		if _, err := f.service.Authenticate(ctx, token, nil); !errors.Is(err, security.ErrUnauthenticated) {
			t.Fatal("token still valid after logout")
		}
	}
	if err := f.service.Logout(ctx, second.CookieToken); err != nil {
		t.Fatal("repeated logout not idempotent")
	}
}

func TestStaleAdvanceAndEpochChangesAreRejected(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	result := f.login(t, security.Secret{})
	digest, _ := security.DigestToken(result.CookieToken)
	snapshot, err := f.store.Lookup(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	stale := snapshot.Session
	if err := f.store.Advance(ctx, digest, stale, stale.Revision, snapshot.Account.AuthVersion, security.DefaultSessionPolicy()); err != nil {
		t.Fatal("valid advance rejected")
	}
	if err := f.store.Advance(ctx, digest, stale, stale.Revision, snapshot.Account.AuthVersion, security.DefaultSessionPolicy()); !errors.Is(err, security.ErrConflict) {
		t.Fatal("stale revision accepted")
	}
	exec(t, f.store.pool, "UPDATE users SET auth_version = auth_version + 1 WHERE id = $1", string(f.user))
	elevated, err := f.service.Authenticate(ctx, result.CookieToken, nil)
	if err != nil || elevated.Principal.CSRFEpoch != result.Principal.CSRFEpoch+1 || elevated.CookieToken.Reveal() == result.CookieToken.Reveal() {
		t.Fatal("privilege change did not rotate session and CSRF epoch")
	}
	exec(t, f.store.pool, "UPDATE users SET active = false WHERE id = $1", string(f.user))
	if _, err := f.service.Authenticate(ctx, elevated.CookieToken, nil); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("inactive account still authenticated")
	}
}

func TestRehashUsesCompareAndSwap(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	record, err := f.store.FindByUsername(ctx, "budi")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Rehash(ctx, f.user, security.NewSecret("bukan-hash-lama"), security.NewSecret("baru"), record.Account.AuthVersion); !errors.Is(err, security.ErrConflict) {
		t.Fatal("rehash overwrote a changed hash")
	}
	if err := f.store.Rehash(ctx, f.user, record.PasswordHash, security.NewSecret("baru"), record.Account.AuthVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.FindByUsername(ctx, "tidak_ada"); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("unknown user not reported as not found")
	}
}

func TestAccessThroughGroupsUsesMaximumGrant(t *testing.T) {
	pool := testPlatform(t)
	ctx := context.Background()
	store, _ := NewStore(pool, newTestClock())
	user := insertUser(t, pool, "siti", false)
	database, other := newID(t), newID(t)
	viewers, editors := newID(t), newID(t)
	exec(t, pool, "INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, 'inventaris', '\\x01'), ($2, 'arsip', '\\x01')", string(database), string(other))
	exec(t, pool, "INSERT INTO groups (id, name) VALUES ($1, 'pengamat'), ($2, 'pengembang')", string(viewers), string(editors))
	exec(t, pool, "INSERT INTO group_memberships (group_id, user_id) VALUES ($1, $3), ($2, $3)", string(viewers), string(editors), string(user))
	exec(t, pool, "INSERT INTO group_database_grants (group_id, database_id, access) VALUES ($1, $3, 'view_only'), ($2, $3, 'view_edit')", string(viewers), string(editors), string(database))
	snapshot, err := store.AccessForDatabase(ctx, user, database)
	if err != nil || snapshot.Account.ID != user {
		t.Fatal(err)
	}
	if access, _ := security.EffectiveAccess(snapshot.GroupAccess); access != security.AccessViewEdit {
		t.Fatal("effective access is not the maximum of group grants")
	}
	if snapshot, _ := store.AccessForDatabase(ctx, user, other); len(snapshot.GroupAccess) != 0 {
		t.Fatal("database without grant returned access")
	}
	principal := security.Principal{UserID: user, SessionID: newID(t), AuthVersion: 1, CSRFEpoch: 1}
	list, err := store.ListAccessible(ctx, principal)
	if err != nil || len(list) != 1 || list[0].ID != database || list[0].Access != security.AccessViewEdit {
		t.Fatal("directory did not list exactly the granted database")
	}
	principal.AuthVersion = 2
	if _, err := store.ListAccessible(ctx, principal); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("stale account epoch listed databases")
	}
}
