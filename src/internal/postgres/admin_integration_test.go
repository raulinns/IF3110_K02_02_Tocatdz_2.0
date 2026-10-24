package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type adminFixture struct {
	pool   *pgxpool.Pool
	store  *AdminStore
	actor  security.Principal
	member security.ID
	group  security.ID
	db     security.ID
}

func liveSession(t *testing.T, pool *pgxpool.Pool, user security.ID) security.ID {
	t.Helper()
	id := newID(t)
	exec(t, pool, `INSERT INTO sessions (id, user_id, auth_version, revision, generation, csrf_epoch, created_at, last_seen_at,
		absolute_expires_at, rotated_at, current_token_sealed) VALUES ($1, $2, 1, 1, 1, 1, now(), now(), now() + interval '1 hour', now(), 'sealed')`, string(id), string(user))
	return id
}

func revoked(t *testing.T, pool *pgxpool.Pool, session security.ID) bool {
	t.Helper()
	var value bool
	if err := pool.QueryRow(context.Background(), "SELECT revoked_at IS NOT NULL FROM sessions WHERE id = $1", string(session)).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	pool := testPlatform(t)
	box, _ := NewSecretBox(testKey(9))
	store, err := NewAdminStore(pool, box, newTestClock())
	if err != nil {
		t.Fatal(err)
	}
	actor := insertUser(t, pool, "admin", true)
	f := &adminFixture{pool: pool, store: store, actor: security.Principal{UserID: actor, SessionID: liveSession(t, pool, actor), AuthVersion: 1, CSRFEpoch: 1}}
	f.member = insertUser(t, pool, "siti", false)
	f.group, f.db = newID(t), newID(t)
	ctx := context.Background()
	if err := store.CreateGroup(ctx, f.actor, admin.Group{ID: f.group, Name: "pengembang"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateConnection(ctx, f.actor, admin.Connection{ID: f.db, Name: "toko"}, security.NewSecret("postgres://u:canary-secret@h/db")); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAdminCreatesStoreEncryptedConnection(t *testing.T) {
	f := newAdminFixture(t)
	var sealed []byte
	if err := f.pool.QueryRow(context.Background(), "SELECT dsn_ciphertext FROM managed_databases WHERE id = $1", string(f.db)).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if len(sealed) == 0 || string(sealed) == "postgres://u:canary-secret@h/db" {
		t.Fatal("connection string stored in plaintext")
	}
	connections, err := f.store.Connections(context.Background())
	if err != nil || len(connections) != 1 || connections[0].Name != "toko" {
		t.Fatal("connection not listed")
	}
}

func TestAdminElevationBumpsEpochWithoutRevoking(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	session := liveSession(t, f.pool, f.member)
	if err := f.store.AddMember(ctx, f.actor, f.group, "siti"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGrant(ctx, f.actor, f.group, f.db, security.AccessViewOnly); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGrant(ctx, f.actor, f.group, f.db, security.AccessViewEdit); err != nil {
		t.Fatal(err)
	}
	if authVersion(t, f.pool, f.member) != 4 || revoked(t, f.pool, session) {
		t.Fatal("elevation must bump epoch for every change without revoking sessions")
	}
	if err := f.store.SetGrant(ctx, f.actor, f.group, f.db, security.AccessViewEdit); err != nil || authVersion(t, f.pool, f.member) != 4 {
		t.Fatal("unchanged grant touched account epoch")
	}
	grants, err := f.store.Grants(ctx)
	if err != nil || len(grants) != 1 || grants[0].Access != security.AccessViewEdit || grants[0].GroupName != "pengembang" {
		t.Fatal("grant not listed")
	}
}

func TestAdminReductionsRevokeAffectedSessions(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		reduce func(f *adminFixture) error
	}{
		{"downgrade grant", func(f *adminFixture) error {
			return f.store.SetGrant(context.Background(), f.actor, f.group, f.db, security.AccessViewOnly)
		}},
		{"revoke grant", func(f *adminFixture) error { return f.store.RevokeGrant(context.Background(), f.actor, f.group, f.db) }},
		{"remove member", func(f *adminFixture) error {
			return f.store.RemoveMember(context.Background(), f.actor, f.group, f.member)
		}},
		{"deactivate user", func(f *adminFixture) error {
			return f.store.SetUserActive(context.Background(), f.actor, f.member, false)
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newAdminFixture(t)
			ctx := context.Background()
			if err := f.store.AddMember(ctx, f.actor, f.group, "siti"); err != nil {
				t.Fatal(err)
			}
			if err := f.store.SetGrant(ctx, f.actor, f.group, f.db, security.AccessViewEdit); err != nil {
				t.Fatal(err)
			}
			session := liveSession(t, f.pool, f.member)
			before := authVersion(t, f.pool, f.member)
			if err := scenario.reduce(f); err != nil {
				t.Fatal(err)
			}
			if authVersion(t, f.pool, f.member) != before+1 || !revoked(t, f.pool, session) {
				t.Fatal("reduction did not bump epoch and revoke sessions atomically")
			}
		})
	}
}

func TestAdminMutationsRecheckActor(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	nonAdmin := security.Principal{UserID: f.member, SessionID: liveSession(t, f.pool, f.member), AuthVersion: 1, CSRFEpoch: 1}
	if err := f.store.CreateGroup(ctx, nonAdmin, admin.Group{ID: newID(t), Name: "liar"}); !errors.Is(err, security.ErrForbidden) {
		t.Fatal("non-admin mutated admin data")
	}
	exec(t, f.pool, "UPDATE users SET auth_version = 2 WHERE id = $1", string(f.actor.UserID))
	if err := f.store.CreateGroup(ctx, f.actor, admin.Group{ID: newID(t), Name: "basi"}); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("stale admin epoch mutated admin data")
	}
	groups, _ := f.store.Groups(ctx)
	if len(groups) != 1 {
		t.Fatal("rejected mutation left data behind")
	}
}

func TestAdminDuplicateAndMissingTargets(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	if err := f.store.CreateGroup(ctx, f.actor, admin.Group{ID: newID(t), Name: "pengembang"}); !errors.Is(err, security.ErrConflict) {
		t.Fatal("duplicate group name not a conflict")
	}
	if err := f.store.AddMember(ctx, f.actor, f.group, "tidak_ada"); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("unknown username not reported")
	}
	if err := f.store.RevokeGrant(ctx, f.actor, f.group, f.db); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("revoking a missing grant not reported")
	}
	if err := f.store.CreateUser(ctx, f.actor, admin.User{ID: newID(t), Username: "siti"}, security.NewSecret("hash")); !errors.Is(err, security.ErrConflict) {
		t.Fatal("duplicate username not a conflict")
	}
	users, err := f.store.Users(ctx)
	if err != nil || len(users) != 2 {
		t.Fatal("users not listed")
	}
}
