package postgres

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestReceiptPrivilegesAndProofAgainstPostgreSQL(t *testing.T) {
	f := newTargetFixture(t)
	ctx := context.Background()
	pool, err := f.targets.pool(ctx, f.database)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var unsafe bool
	if err := conn.QueryRow(ctx, "SELECT rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&unsafe); err != nil || unsafe {
		t.Fatal("SQL runtime role is privileged")
	}
	for _, sql := range []string{
		"SELECT * FROM tocatdz_internal.receipt_keys",
		"DELETE FROM tocatdz_internal.migration_receipts",
		"TRUNCATE tocatdz_internal.migration_receipts",
		"ALTER TABLE tocatdz_internal.migration_receipts ADD COLUMN forged bool",
		"DROP SCHEMA tocatdz_internal CASCADE",
		"CREATE ROLE escaped SUPERUSER",
		"SET ROLE postgres",
		"SELECT tocatdz_internal.write_receipt('00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000002','00000000-0000-4000-8000-000000000003',1,decode(repeat('00',32),'hex'),decode(repeat('00',32),'hex'))",
	} {
		if _, err := conn.Exec(ctx, sql); err == nil {
			t.Fatalf("restricted runtime accepted %s", sql)
		}
	}
	if _, err := conn.Exec(ctx, "RESET ROLE"); err != nil {
		t.Fatal(err)
	}
	if err := targetReady(ctx, conn.Conn(), f.targets.box); err != nil {
		t.Fatal("reset role escaped restricted session")
	}
	var count int
	if err := f.target.QueryRow(ctx, "SELECT count(*) FROM tocatdz_internal.migration_receipts").Scan(&count); err != nil || count != 0 {
		t.Fatal("forged receipt was stored")
	}
}

func TestPrepareTargetIsIdempotentAndRejectsMetadata(t *testing.T) {
	ctx := context.Background()
	platform := testPlatform(t)
	dsn := createDatabase(t)
	parsed, _ := url.Parse(dsn)
	query := parsed.Query()
	query.Set("user", parsed.User.Username())
	parsed.RawQuery = query.Encode()
	dsn = parsed.String()
	box, _ := NewSecretBox(testKey(9))
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE EXTENSION pgcrypto WITH SCHEMA public"); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	admin.Close(ctx)
	one, err := PrepareManagedTarget(ctx, platform, security.NewSecret(dsn), box)
	if err != nil {
		t.Fatal(err)
	}
	two, err := PrepareManagedTarget(ctx, platform, security.NewSecret(dsn), box)
	if err != nil || one.Reveal() != two.Reveal() {
		t.Fatal("setup changed runtime credentials")
	}
	conn, err := pgx.Connect(ctx, two.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := targetReady(ctx, conn, box); err != nil {
		t.Fatal(err)
	}
	wrong, _ := NewSecretBox(testKey(8))
	if _, err := PrepareManagedTarget(ctx, platform, security.NewSecret(dsn), wrong); !errors.Is(err, security.ErrTargetNotReady) {
		t.Fatal("setup accepted another application key")
	}
	metadata := security.NewSecret(platform.Config().ConnConfig.ConnString())
	if _, err := PrepareManagedTarget(ctx, platform, metadata, box); !errors.Is(err, security.ErrTargetNotReady) {
		t.Fatal("metadata database accepted as target")
	}
	var exists bool
	if err := platform.QueryRow(ctx, "SELECT to_regnamespace('tocatdz_internal') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatal("rejected setup modified metadata database")
	}
}

type lostCommitCoordinator struct{ migration.Coordinator }
type lostCommitTarget struct{ migration.LockedTarget }
type lostCommitTx struct{ migration.Transaction }

func (c lostCommitCoordinator) WithDatabaseLock(ctx context.Context, id security.ID, run func(context.Context, migration.LockedTarget) error) error {
	return c.Coordinator.WithDatabaseLock(ctx, id, func(ctx context.Context, target migration.LockedTarget) error {
		return run(ctx, lostCommitTarget{target})
	})
}
func (t lostCommitTarget) Begin(ctx context.Context) (migration.Transaction, error) {
	tx, err := t.LockedTarget.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return lostCommitTx{tx}, nil
}
func (t lostCommitTx) Commit(ctx context.Context) error {
	if err := t.Transaction.Commit(ctx); err != nil {
		return err
	}
	return security.ErrUnavailable
}

func TestRecoveryAfterActualCommitAcknowledgementIsLost(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()
	accounts, _ := NewStore(f.platform, f.clock)
	service, err := migration.NewService(migration.Config{Repository: f.ledger, Coordinator: lostCommitCoordinator{f.targets}, Validator: migration.NewPostgreSQLValidator(), Authorizer: allowAll{}, Accounts: accounts, IDs: security.NewTokenGenerator(), Clock: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	id := f.draft(t, "Commit tanpa jawaban", "CREATE TABLE public.only_once(id int PRIMARY KEY); INSERT INTO public.only_once VALUES(1);")
	result, err := service.Execute(ctx, f.request, id, 1)
	if !errors.Is(err, migration.ErrExecutionUncertain) || result.Status != migration.Pending || !f.tableExists(t, "public.only_once") {
		t.Fatal("lost acknowledgement did not leave a real committed pending migration")
	}
	f.targets.Close()
	restarted, err := NewTargets(f.platform, f.targets.box)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	reconciler, _ := migration.NewReconciler(migration.ReconcilerConfig{Repository: f.ledger, Coordinator: restarted, Clock: f.clock})
	worker, _ := migration.NewReconcileWorker(f.ledger, reconciler, time.Minute)
	if worker.Sweep(ctx) != 1 {
		t.Fatal("restarted worker did not recover committed migration")
	}
	row, err := f.ledger.Find(ctx, f.database, id)
	if err != nil || row.Status != migration.Applied {
		t.Fatal("receipt did not restore applied status")
	}
	var count int
	if err := f.target.QueryRow(ctx, "SELECT count(*) FROM public.only_once").Scan(&count); err != nil || count != 1 {
		t.Fatal("recovery executed SQL twice")
	}
	if worker.Sweep(ctx) != 0 {
		t.Fatal("finished migration was recovered again")
	}
}

func TestReceiptProbeWaitsForRealTransactionAndDetectsDisconnect(t *testing.T) {
	f := newTargetFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := migration.ExecutionKey{DatabaseID: f.database, MigrationID: newID(t), ExecutionID: newID(t), Version: 1}
	pool, err := f.targets.pool(ctx, f.database)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	abandoned := &targetTx{tx: tx}
	defer abandoned.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SELECT pg_catalog.pg_advisory_xact_lock($1)", int64(targetExecutionLockKey)); err != nil {
		t.Fatal(err)
	}
	if err := abandoned.Execute(ctx, security.NewSecret("CREATE TABLE public.not_committed(id int)")); err != nil {
		t.Fatal(err)
	}
	probe := func(want bool) {
		t.Helper()
		if err := f.targets.WithDatabaseLock(ctx, f.database, func(ctx context.Context, target migration.LockedTarget) error {
			lookup, err := target.LookupReceipt(ctx, key)
			if err != nil {
				return err
			}
			if lookup.Quiescent != want || lookup.Receipt != nil {
				t.Fatalf("unexpected receipt probe: %+v", lookup)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	probe(false)
	if err := abandoned.tx.Conn().Close(ctx); err != nil {
		t.Fatal(err)
	}
	probe(true)
	var exists bool
	if err := f.target.QueryRow(ctx, "SELECT to_regclass('public.not_committed') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatal("disconnected SQL transaction was not rolled back")
	}
}
