package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Authorizer test: hak View + Edit selalu diberikan; otorisasi asli sudah diuji di paket security dan UI.
type allowAll struct{}

func (allowAll) RequirePlatformAdmin(context.Context, security.Principal) error { return nil }
func (allowAll) RequireDatabase(context.Context, security.Principal, security.ID, security.DatabaseAction) error {
	return nil
}

type migrationFixture struct {
	*targetFixtureEnv
	ledger    *MigrationStore
	service   *migration.Service
	request   migration.Request
	clock     *testClock
	principal security.Principal
}

func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	env := newTargetFixture(t)
	clock := newTestClock()
	store, _ := NewStore(env.platform, clock)
	ledger, err := NewMigrationStore(env.platform)
	if err != nil {
		t.Fatal(err)
	}
	user := insertUser(t, env.platform, "budi", false)
	session := newID(t)
	exec(t, env.platform, `INSERT INTO sessions (id, user_id, auth_version, revision, generation, csrf_epoch, created_at, last_seen_at,
		absolute_expires_at, rotated_at, current_token_sealed) VALUES ($1, $2, 1, 1, 1, 1, now(), now(), now() + interval '1 hour', now(), 'sealed')`, string(session), string(user))
	service, err := migration.NewService(migration.Config{Repository: ledger, Coordinator: env.targets, Validator: migration.NewPostgreSQLValidator(),
		Authorizer: allowAll{}, Accounts: store, IDs: security.NewTokenGenerator(), Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	principal := security.Principal{UserID: user, SessionID: session, AuthVersion: 1, CSRFEpoch: 1}
	return &migrationFixture{targetFixtureEnv: env, ledger: ledger, service: service, clock: clock, principal: principal,
		request: migration.Request{Principal: principal, DatabaseID: env.database}}
}

func (f *migrationFixture) draft(t *testing.T, title, sql string) security.ID {
	t.Helper()
	id, err := f.service.CreateDraft(context.Background(), f.request, migration.Draft{Title: title, SQL: security.NewSecret(sql)})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.add(time.Second)
	return id
}

func (f *migrationFixture) tableExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	if err := f.target.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestMigrationCommitWritesLedgerAndReceipt(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()
	id := f.draft(t, "Buat tabel produk", "CREATE TABLE public.produk (id integer PRIMARY KEY); INSERT INTO public.produk VALUES (1), (2);")
	if err := f.service.UpdateDraft(ctx, f.request, id, migration.Draft{Title: "Buat tabel produk", SQL: security.NewSecret("CREATE TABLE public.produk (id integer PRIMARY KEY, nama text); INSERT INTO public.produk VALUES (1, 'a');")}, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.service.UpdateDraft(ctx, f.request, id, migration.Draft{Title: "basi", SQL: security.NewSecret("SELECT 1")}, 1); !errors.Is(err, security.ErrConflict) {
		t.Fatal("stale draft revision accepted")
	}
	if _, err := f.service.Execute(ctx, f.request, id, 1); !errors.Is(err, security.ErrConflict) {
		t.Fatal("execute with stale revision accepted")
	}
	result, err := f.service.Execute(ctx, f.request, id, 2)
	if err != nil || result.Status != migration.Applied || result.Version != 1 {
		t.Fatalf("execute failed: %+v %v", result, err)
	}
	if !f.tableExists(t, "public.produk") {
		t.Fatal("committed migration did not change target")
	}
	record, err := f.service.Migration(ctx, f.request, id)
	if err != nil || record.Status != migration.Applied || record.Actor != "budi" || record.ActorID != f.principal.UserID || record.ExecutedAt.IsZero() {
		t.Fatalf("ledger missing execution metadata: %+v", record.Summary)
	}
	var receipts int
	if err := f.target.QueryRow(ctx, "SELECT count(*) FROM tocatdz_internal.migration_receipts WHERE execution_id = $1", string(record.ExecutionID)).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatal("execution receipt not stored in target transaction")
	}
	if _, err := f.service.Execute(ctx, f.request, id, record.Revision); !errors.Is(err, security.ErrConflict) {
		t.Fatal("applied migration executed twice")
	}
	if _, err := f.platform.Exec(ctx, "UPDATE migrations SET sql = 'DROP TABLE x' WHERE id = $1", string(id)); err == nil {
		t.Fatal("executed migration SQL could be rewritten")
	}
}

func TestMigrationFailureRollsBackEverything(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()
	id := f.draft(t, "Gagal di tengah", "CREATE TABLE public.sementara (id integer); INSERT INTO public.tidak_ada VALUES (1);")
	result, err := f.service.Execute(ctx, f.request, id, 1)
	if err != nil || result.Status != migration.Failed || result.Failure != migration.FailureSQL {
		t.Fatalf("failed migration not recorded: %+v %v", result, err)
	}
	if f.tableExists(t, "public.sementara") {
		t.Fatal("partial migration effect survived rollback")
	}
	ok := f.draft(t, "Berikutnya", "CREATE TABLE public.berikutnya (id integer);")
	result, err = f.service.Execute(ctx, f.request, ok, 1)
	if err != nil || result.Version != 2 {
		t.Fatal("version after failed attempt not monotonic")
	}
	page, err := f.service.History(ctx, f.request, migration.HistoryQuery{})
	if err != nil || len(page.Items) != 2 || page.Items[0].Version != 2 || page.Items[1].Status != migration.Failed {
		t.Fatalf("history not linear: %+v %v", page.Items, err)
	}
}

func TestMigrationValidatorRejectsUnsafeSQLBeforeTarget(t *testing.T) {
	f := newMigrationFixture(t)
	for _, sql := range []string{"BEGIN; CREATE TABLE public.x (id int); COMMIT;", "CREATE TABLE tocatdz_internal.palsu (id int);", "DROP DATABASE postgres;"} {
		id := f.draft(t, "Tidak aman", sql)
		if _, err := f.service.Execute(context.Background(), f.request, id, 1); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatalf("unsafe SQL accepted: %s (%v)", sql, err)
		}
	}
	if f.tableExists(t, "public.x") {
		t.Fatal("rejected SQL reached target")
	}
}

func TestMigrationsAreScopedToDatabase(t *testing.T) {
	f := newMigrationFixture(t)
	id := f.draft(t, "Rahasia", "SELECT 1;")
	other := f.request
	other.DatabaseID = newID(t)
	if _, err := f.service.Migration(context.Background(), other, id); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("migration readable through another database id")
	}
}

func TestStaleActorCannotMutateLedger(t *testing.T) {
	f := newMigrationFixture(t)
	exec(t, f.platform, "UPDATE users SET auth_version = 2 WHERE id = $1", string(f.principal.UserID))
	if _, err := f.service.CreateDraft(context.Background(), f.request, migration.Draft{Title: "x", SQL: security.NewSecret("SELECT 1;")}); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("stale epoch created a draft")
	}
}

func TestReconcilerResolvesPendingFromReceipt(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()
	applied := f.draft(t, "Sudah commit", "CREATE TABLE public.r1 (id int);")
	if _, err := f.service.Execute(ctx, f.request, applied, 1); err != nil {
		t.Fatal(err)
	}
	// Simulasi crash: ledger kembali pending walaupun receipt di target sudah ada.
	exec(t, f.platform, "ALTER TABLE migrations DISABLE TRIGGER migrations_protect_history")
	exec(t, f.platform, "DROP INDEX migrations_one_pending_idx")
	exec(t, f.platform, "UPDATE migrations SET status = 'pending' WHERE id = $1", string(applied))
	lost := f.draft(t, "Tidak sempat", "CREATE TABLE public.r2 (id int);")
	exec(t, f.platform, `UPDATE migrations SET status = 'pending', version = 99, execution_id = $2, checksum = sha256(convert_to(sql, 'UTF8')),
		actor_id = created_by, actor = 'budi', executed_at = now(), revision = 2 WHERE id = $1`, string(lost), string(newID(t)))
	exec(t, f.platform, "ALTER TABLE migrations ENABLE TRIGGER migrations_protect_history")
	reconciler, err := migration.NewReconciler(migration.ReconcilerConfig{Repository: f.ledger, Coordinator: f.targets, Clock: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := migration.NewReconcileWorker(f.ledger, reconciler, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if resolved := worker.Sweep(ctx); resolved != 2 {
		t.Fatalf("expected 2 resolved migrations, got %d", resolved)
	}
	first, _ := f.service.Migration(ctx, f.request, applied)
	second, _ := f.service.Migration(ctx, f.request, lost)
	if first.Status != migration.Applied || second.Status != migration.Failed || second.Failure != migration.FailureInterrupted {
		t.Fatalf("reconciliation wrong: %s %s", first.Status, second.Status)
	}
	if f.tableExists(t, "public.r2") {
		t.Fatal("reconciler re-ran SQL")
	}
}
