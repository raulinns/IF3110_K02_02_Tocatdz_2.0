package platformstore

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb"
	platformsqlc "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb/sqlc"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/pgtest"
)

const (
	testUserID     security.ID = "11111111-1111-4111-8111-111111111111"
	testSessionID  security.ID = "22222222-2222-4222-8222-222222222222"
	testGroupView  security.ID = "33333333-3333-4333-8333-333333333333"
	testGroupEdit  security.ID = "44444444-4444-4444-8444-444444444444"
	testDatabaseID security.ID = "55555555-5555-4555-8555-555555555555"
	testInventory  security.ID = "77777777-7777-4777-8777-777777777777"
)

func integrationStore(t *testing.T) (*Store, *platformsqlc.Queries, func()) {
	t.Helper()

	databaseURL := pgtest.Database(t, "TEST_DATABASE_URL")

	ctx := context.Background()
	pool, err := platformdb.OpenMigrated(ctx, platformdb.Config{URL: databaseURL})
	if err != nil {
		t.Fatalf("open migrated test database: %v", err)
	}
	_, err = pool.Exec(ctx, `
TRUNCATE migration_history, sessions, group_database_access,
         user_groups, managed_databases, groups, users
RESTART IDENTITY CASCADE`)
	if err != nil {
		pool.Close()
		t.Fatalf("reset test database: %v", err)
	}

	store, err := New(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("New() returned error: %v", err)
	}
	return store, platformsqlc.New(pool), pool.Close
}

func TestTargetCatalogDecryptsManagedDatabaseConnection(t *testing.T) {
	store, queries, closePool := integrationStore(t)
	defer closePool()
	seedAccessFixture(t, queries, store)

	protector, err := dbms.NewConnectionProtector(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	wantDSN := security.NewSecret("postgres://reader:private@target/inventory")
	ciphertext, err := protector.Seal(context.Background(), testDatabaseID, wantDSN)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(
		context.Background(),
		"UPDATE managed_databases SET connection_secret = $2 WHERE id = $1",
		string(testDatabaseID),
		ciphertext,
	)
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := NewTargetCatalog(store, protector)
	if err != nil {
		t.Fatal(err)
	}
	target, err := catalog.Target(context.Background(), testDatabaseID)
	if err != nil || target.Engine != dbms.PostgreSQL || target.DSN.Reveal() != wantDSN.Reveal() || !target.Active {
		t.Fatalf("Target() = (%+v, %v)", target, err)
	}
}

func TestTargetCatalogRegistersEncryptedDatabaseAndGrant(t *testing.T) {
	store, queries, closePool := integrationStore(t)
	defer closePool()
	seedAccessFixture(t, queries, store)

	protector, err := dbms.NewConnectionProtector(bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewTargetCatalog(store, protector)
	if err != nil {
		t.Fatal(err)
	}
	dsn := security.NewSecret("postgres://root:private@db:5432/inventory")
	input := ManagedDatabaseInput{
		ID:     testInventory,
		Name:   "inventory arsip",
		Engine: dbms.PostgreSQL,
		DSN:    dsn,
		Active: true,
	}
	if err := catalog.Register(context.Background(), input); err != nil {
		t.Fatalf("Register() returned error: %v", err)
	}

	var stored []byte
	if err := store.pool.QueryRow(
		context.Background(),
		"SELECT connection_secret FROM managed_databases WHERE id = $1",
		string(testInventory),
	).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("private")) {
		t.Fatal("Register() persisted plaintext credentials")
	}
	target, err := catalog.Target(context.Background(), testInventory)
	if err != nil || target.Engine != dbms.PostgreSQL || target.DSN.Reveal() != dsn.Reveal() {
		t.Fatalf("Target() = (%+v, %v)", target, err)
	}

	if err := store.GrantGroupAccess(context.Background(), testGroupView, testInventory, security.AccessViewOnly); err != nil {
		t.Fatalf("GrantGroupAccess() returned error: %v", err)
	}
	access, err := store.AccessForDatabase(context.Background(), testUserID, testInventory)
	if err != nil || len(access.GroupAccess) != 1 || access.GroupAccess[0] != security.AccessViewOnly {
		t.Fatalf("AccessForDatabase() = (%+v, %v)", access, err)
	}
	if err := catalog.Register(context.Background(), input); err != security.ErrConflict {
		t.Fatalf("duplicate Register() error = %v, want ErrConflict", err)
	}
}

func seedAccessFixture(t *testing.T, queries *platformsqlc.Queries, store *Store) {
	t.Helper()
	ctx := context.Background()

	err := queries.CreateUser(ctx, platformsqlc.CreateUserParams{
		ID:            string(testUserID),
		Username:      "alice",
		PasswordHash:  "argon2id-hash",
		Active:        true,
		PlatformAdmin: false,
		AuthVersion:   1,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	statements := []struct {
		sql  string
		args []any
	}{
		{
			sql:  "INSERT INTO groups (id, name) VALUES ($1, 'viewer'), ($2, 'editor')",
			args: []any{string(testGroupView), string(testGroupEdit)},
		},
		{
			sql:  "INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2), ($1, $3)",
			args: []any{string(testUserID), string(testGroupView), string(testGroupEdit)},
		},
		{
			sql: "INSERT INTO managed_databases (id, name, engine, connection_secret) " +
				"VALUES ($1, 'inventory', 'postgresql', decode('01', 'hex'))",
			args: []any{string(testDatabaseID)},
		},
		{
			sql: "INSERT INTO group_database_access (group_id, database_id, access_level) " +
				"VALUES ($1, $3, 'view_only'), ($2, $3, 'view_edit')",
			args: []any{string(testGroupView), string(testGroupEdit), string(testDatabaseID)},
		},
	}
	for _, statement := range statements {
		if _, err := store.pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed access fixture: %v", err)
		}
	}
}

func TestStorePersistsCredentialsAccessAndDirectory(t *testing.T) {
	store, queries, closePool := integrationStore(t)
	defer closePool()
	seedAccessFixture(t, queries, store)

	ctx := context.Background()
	credential, err := store.FindByUsername(ctx, "alice")
	if err != nil || credential.Account.ID != testUserID || credential.PasswordHash.Reveal() != "argon2id-hash" {
		t.Fatalf("FindByUsername() = (%+v, %v)", credential.Account, err)
	}
	if err := store.Rehash(ctx, testUserID, security.NewSecret("argon2id-hash"), security.NewSecret("replacement-hash"), 1); err != nil {
		t.Fatalf("Rehash() returned error: %v", err)
	}
	if err := store.Rehash(ctx, testUserID, security.NewSecret("stale-hash"), security.NewSecret("other-hash"), 1); err != security.ErrConflict {
		t.Fatalf("stale Rehash() error = %v, want ErrConflict", err)
	}

	access, err := store.AccessForDatabase(ctx, testUserID, testDatabaseID)
	if err != nil || len(access.GroupAccess) != 2 {
		t.Fatalf("AccessForDatabase() = (%+v, %v)", access, err)
	}
	databases, err := store.ListAccessible(ctx, security.Principal{UserID: testUserID})
	if err != nil || len(databases) != 1 || databases[0].Access != security.AccessViewEdit {
		t.Fatalf("ListAccessible() = (%+v, %v)", databases, err)
	}
}

func TestStorePersistsSessionLifecycle(t *testing.T) {
	store, queries, closePool := integrationStore(t)
	defer closePool()
	seedAccessFixture(t, queries, store)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	policy := security.DefaultSessionPolicy()
	currentDigest := security.TokenDigest{1}
	session := security.Session{
		ID:                 testSessionID,
		UserID:             testUserID,
		AuthVersion:        1,
		Revision:           1,
		Generation:         1,
		CSRFEpoch:          1,
		CreatedAt:          now,
		LastSeenAt:         now,
		AbsoluteExpiresAt:  now.Add(policy.AbsoluteLifetime),
		RotatedAt:          now,
		CurrentDigest:      currentDigest,
		CurrentTokenSealed: security.NewSecret("sealed-token"),
	}

	if err := store.Create(ctx, session, 1, nil, policy); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	snapshot, err := store.Lookup(ctx, currentDigest)
	if err != nil || snapshot.Session.ID != testSessionID || snapshot.PresentedPrevious {
		t.Fatalf("Lookup() = (%+v, %v)", snapshot, err)
	}

	replacement := snapshot.Session
	newDigest := security.TokenDigest{2}
	replacement.Generation = 2
	replacement.LastSeenAt = now.Add(15 * time.Minute)
	replacement.RotatedAt = replacement.LastSeenAt
	replacement.CurrentDigest = newDigest
	replacement.PreviousDigest = &currentDigest
	replacement.PreviousValidUntil = replacement.RotatedAt.Add(policy.GracePeriod)
	replacement.CurrentTokenSealed = security.NewSecret("replacement-sealed-token")

	if err := store.Advance(ctx, currentDigest, replacement, 1, 1, policy); err != nil {
		t.Fatalf("Advance() returned error: %v", err)
	}
	snapshot, err = store.Lookup(ctx, currentDigest)
	if err != nil || !snapshot.PresentedPrevious || snapshot.Session.Generation != 2 || snapshot.Session.Revision != 2 {
		t.Fatalf("Lookup(previous) = (%+v, %v)", snapshot, err)
	}

	if err := store.Revoke(ctx, testSessionID, replacement.LastSeenAt.Add(time.Second)); err != nil {
		t.Fatalf("Revoke() returned error: %v", err)
	}
	snapshot, err = store.FindByID(ctx, testSessionID)
	if err != nil || snapshot.Session.RevokedAt == nil {
		t.Fatalf("FindByID() after revoke = (%+v, %v)", snapshot, err)
	}
}

func TestStorePersistsMigrationLifecycle(t *testing.T) {
	store, queries, closePool := integrationStore(t)
	defer closePool()
	seedAccessFixture(t, queries, store)

	ctx := context.Background()
	first, err := store.CreateMigration(
		ctx,
		testDatabaseID,
		testUserID,
		"CREATE TABLE products (id BIGINT PRIMARY KEY)",
	)
	if err != nil || first.Version != 1 || first.Status != MigrationPending {
		t.Fatalf("CreateMigration(first) = (%+v, %v)", first, err)
	}
	second, err := store.CreateMigration(
		ctx,
		testDatabaseID,
		testUserID,
		"ALTER TABLE products ADD COLUMN name TEXT",
	)
	if err != nil || second.Version != 2 {
		t.Fatalf("CreateMigration(second) = (%+v, %v)", second, err)
	}

	if err := store.MarkMigrationRunning(ctx, first.ID); err != nil {
		t.Fatalf("MarkMigrationRunning() returned error: %v", err)
	}
	if err := store.FinishMigration(ctx, first.ID, MigrationCommitted, ""); err != nil {
		t.Fatalf("FinishMigration() returned error: %v", err)
	}
	if err := store.FinishMigration(ctx, second.ID, MigrationCommitted, ""); err != security.ErrConflict {
		t.Fatalf("FinishMigration(pending) error = %v, want ErrConflict", err)
	}
	if err := store.FinishMigration(ctx, second.ID, MigrationPending, ""); err != security.ErrInvalidInput {
		t.Fatalf("FinishMigration(invalid status) error = %v, want ErrInvalidInput", err)
	}

	history, err := store.ListMigrations(ctx, testDatabaseID)
	if err != nil || len(history) != 2 {
		t.Fatalf("ListMigrations() = (%+v, %v)", history, err)
	}
	if history[0].Version != 2 || history[1].Version != 1 || history[1].Status != MigrationCommitted {
		t.Fatalf("ListMigrations() returned unexpected order/state: %+v", history)
	}
}
