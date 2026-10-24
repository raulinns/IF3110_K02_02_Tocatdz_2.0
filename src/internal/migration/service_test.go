package migration_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/migrationtest"
)

const (
	db      security.ID = "3f1c2d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	otherDB security.ID = "6a7b8c9d-0e1f-4a2b-9c3d-4e5f6a7b8c9d"
	user    security.ID = "b0c4f0c0-9167-43d6-9089-afd7b9826f0d"
	session security.ID = "5ed48df2-bcdd-4b52-b9ad-8d9f6689df55"
)

var req = migration.Request{DatabaseID: db, Principal: security.Principal{UserID: user, SessionID: session, AuthVersion: 1, CSRFEpoch: 1}}

type authorization struct {
	access  atomic.Int32
	denied  atomic.Bool
	account security.Account
}

func (a *authorization) RequirePlatformAdmin(context.Context, security.Principal) error {
	return security.ErrForbidden
}
func (a *authorization) RequireDatabase(ctx context.Context, p security.Principal, database security.ID, action security.DatabaseAction) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if a.denied.Load() {
		return security.ErrUnauthenticated
	}
	if database != db && database != otherDB {
		return security.ErrForbidden
	}
	access := security.AccessViewEdit
	if a.access.Load() == 1 {
		access = security.AccessViewOnly
	}
	if !security.Allows(access, action) {
		return security.ErrForbidden
	}
	return nil
}
func (a *authorization) FindAccount(context.Context, security.ID) (security.Account, error) {
	return a.account, nil
}
func setup(t *testing.T) (*migration.Service, *migrationtest.Fixture, *authorization) {
	t.Helper()
	fixture := migrationtest.New()
	auth := &authorization{account: security.Account{ID: user, Username: "budi", Active: true, AuthVersion: 1}}
	fixture.Check = func(ctx context.Context, request migration.Request) error {
		return auth.RequireDatabase(ctx, request.Principal, request.DatabaseID, security.ActionExecuteMigration)
	}
	service, err := migration.NewService(migration.Config{Repository: fixture, Coordinator: fixture, Validator: migration.NewPostgreSQLValidator(), Authorizer: auth, Accounts: auth, IDs: security.NewTokenGenerator(), Clock: security.SystemClock{}})
	if err != nil {
		t.Fatal(err)
	}
	return service, fixture, auth
}
func draft(t *testing.T, s *migration.Service) security.ID {
	t.Helper()
	id, err := s.CreateDraft(context.Background(), req, migration.Draft{Title: "Buat tabel", SQL: security.NewSecret("CREATE TABLE public.items (id integer PRIMARY KEY);")})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestDraftRevisionAndDatabaseScope(t *testing.T) {
	s, f, auth := setup(t)
	id := draft(t, s)
	updated := migration.Draft{Title: "Ubah tabel", SQL: security.NewSecret("  CREATE TABLE public.updated (id int);  ")}
	if err := s.UpdateDraft(context.Background(), req, id, updated, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDraft(context.Background(), req, id, updated, 1); !errors.Is(err, security.ErrConflict) {
		t.Fatal("stale revision accepted")
	}
	record, err := s.Migration(context.Background(), req, id)
	if err != nil || record.Revision != 2 || record.SQL.Reveal() != updated.SQL.Reveal() {
		t.Fatal("draft not preserved")
	}
	other := req
	other.DatabaseID = otherDB
	if _, err := s.Migration(context.Background(), other, id); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("cross-database migration visible")
	}
	if err := s.UpdateDraft(context.Background(), other, id, updated, 2); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("cross-database update accepted")
	}
	auth.access.Store(1)
	if _, err := s.History(context.Background(), req, migration.HistoryQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDraft(context.Background(), req, updated); !errors.Is(err, security.ErrForbidden) {
		t.Fatal("viewer created draft")
	}
	if _, err := s.Execute(context.Background(), req, id, 2); !errors.Is(err, security.ErrForbidden) || f.Snapshot().Begins != 0 {
		t.Fatal("viewer executed SQL")
	}
}
func TestExecuteOutcomesAndNoBlindRetry(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		configure func(*migrationtest.Fixture)
		status    migration.Status
		uncertain bool
	}{
		{"commit", func(*migrationtest.Fixture) {}, migration.Applied, false},
		{"SQL rollback", func(f *migrationtest.Fixture) { f.SQLError = errors.New("driver canary-secret") }, migration.Failed, false},
		{"receipt rollback", func(f *migrationtest.Fixture) { f.ReceiptError = errors.New("receipt canary-secret") }, migration.Failed, false},
		{"begin failure", func(f *migrationtest.Fixture) { f.BeginError = security.ErrUnavailable }, migration.Failed, false},
		{"rollback uncertain", func(f *migrationtest.Fixture) {
			f.SQLError = security.ErrUnavailable
			f.RollbackError = security.ErrUnavailable
		}, migration.Pending, true},
		{"commit uncertain", func(f *migrationtest.Fixture) { f.CommitError = errors.New("wire canary-secret") }, migration.Pending, true},
		{"ledger uncertain", func(f *migrationtest.Fixture) { f.FinishError = errors.New("ledger canary-secret") }, migration.Pending, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, f, _ := setup(t)
			id := draft(t, s)
			scenario.configure(f)
			result, err := s.Execute(context.Background(), req, id, 1)
			if result.Status != scenario.status || errors.Is(err, migration.ErrExecutionUncertain) != scenario.uncertain {
				t.Fatalf("unexpected result %v %v", result, err)
			}
			if !scenario.uncertain && err != nil {
				t.Fatal(err)
			}
			if strings.Contains(fmt.Sprint(err, result), "canary-secret") {
				t.Fatal("driver error leaked")
			}
			before := f.Snapshot().Executes
			if _, err := s.Execute(context.Background(), req, id, 1); err == nil {
				t.Fatal("terminal or pending execution retried")
			}
			if f.Snapshot().Executes != before {
				t.Fatal("SQL executed twice")
			}
			if err := s.UpdateDraft(context.Background(), req, id, migration.Draft{Title: "new", SQL: security.NewSecret("SELECT 1")}, 2); !errors.Is(err, security.ErrConflict) {
				t.Fatal("executed history changed")
			}
		})
	}
}
func TestReconcileNeverRunsSQL(t *testing.T) {
	for _, scenario := range []struct {
		name                         string
		applied, quiescent, mismatch bool
		want                         migration.Status
	}{
		{"receipt committed", true, true, false, migration.Applied},
		{"confirmed absent", false, true, false, migration.Failed},
		{"still running", false, false, false, migration.Pending},
		{"wrong receipt", true, true, true, migration.Pending},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, f, _ := setup(t)
			id := draft(t, s)
			f.CommitError = security.ErrUnavailable
			f.CommitDespiteError = scenario.applied
			if _, err := s.Execute(context.Background(), req, id, 1); !errors.Is(err, migration.ErrExecutionUncertain) {
				t.Fatal("expected pending")
			}
			f.ProbeNotQuiescent = !scenario.quiescent
			f.ProbeMismatch = scenario.mismatch
			r, err := migration.NewReconciler(migration.ReconcilerConfig{Repository: f, Coordinator: f, Clock: security.SystemClock{}})
			if err != nil {
				t.Fatal(err)
			}
			before := f.Snapshot().Executes
			result, err := r.Reconcile(context.Background(), db, id)
			if result.Status != scenario.want || f.Snapshot().Executes != before {
				t.Fatal("wrong recovery or SQL rerun")
			}
			if scenario.want == migration.Pending && !errors.Is(err, migration.ErrExecutionUncertain) {
				t.Fatal("uncertainty lost")
			}
			if scenario.want != migration.Pending {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.Reconcile(context.Background(), db, id); err != nil {
					t.Fatal("recovery not idempotent")
				}
			}
		})
	}
}
func TestRevocationAfterLockPreventsDispatch(t *testing.T) {
	s, f, auth := setup(t)
	id := draft(t, s)
	f.AfterLock = func() { auth.denied.Store(true) }
	if _, err := s.Execute(context.Background(), req, id, 1); !errors.Is(err, security.ErrUnauthenticated) || f.Snapshot().Begins != 0 {
		t.Fatal("revoked access opened transaction")
	}
}
func TestCancelledRequestStillRollsBackAndRecords(t *testing.T) {
	s, f, _ := setup(t)
	id := draft(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.AfterSQL = cancel
	result, err := s.Execute(ctx, req, id, 1)
	if err != nil || result.Status != migration.Failed || f.Snapshot().Rollbacks != 1 {
		t.Fatal("cancelled SQL did not clean up")
	}
}
func TestConcurrentExecuteAndUpdateCAS(t *testing.T) {
	s, f, _ := setup(t)
	id := draft(t, s)
	var group sync.WaitGroup
	var successes atomic.Int32
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := s.Execute(context.Background(), req, id, 1); err == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 || f.Snapshot().Executes != 1 {
		t.Fatal("concurrent execute ran SQL twice")
	}
	id = draft(t, s)
	successes.Store(0)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			if s.UpdateDraft(context.Background(), req, id, migration.Draft{Title: "update", SQL: security.NewSecret("SELECT 1")}, 1) == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatal("CAS allowed concurrent stale update")
	}
}
func TestHistoryPaginationAndVersions(t *testing.T) {
	s, _, _ := setup(t)
	for range 3 {
		id := draft(t, s)
		if _, err := s.Execute(context.Background(), req, id, 1); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.History(context.Background(), req, migration.HistoryQuery{Page: 1, Limit: 2})
	if err != nil || !page.HasNext || len(page.Items) != 2 || page.Items[0].Version != 3 || page.Items[1].Version != 2 {
		t.Fatal("history order or pagination wrong")
	}
	page, err = s.History(context.Background(), req, migration.HistoryQuery{Page: 2, Limit: 2})
	if err != nil || page.HasNext || len(page.Items) != 1 || page.Items[0].Version != 1 {
		t.Fatal("second history page wrong")
	}
}
func TestRejectedSQLDoesNotReserveOrExecute(t *testing.T) {
	s, f, _ := setup(t)
	id, err := s.CreateDraft(context.Background(), req, migration.Draft{Title: "bad", SQL: security.NewSecret("COMMIT;")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(context.Background(), req, id, 1); !errors.Is(err, security.ErrInvalidInput) || f.Snapshot().Begins != 0 {
		t.Fatal("unsafe SQL dispatched")
	}
	record, _ := s.Migration(context.Background(), req, id)
	if record.Status != migration.DraftStatus || record.Revision != 1 {
		t.Fatal("rejected SQL consumed draft")
	}
}

func TestEditBetweenValidationAndClaimPreventsExecution(t *testing.T) {
	s, f, _ := setup(t)
	id := draft(t, s)
	f.BeforeReserve = func() {
		if err := s.UpdateDraft(context.Background(), req, id, migration.Draft{Title: "new", SQL: security.NewSecret("SELECT 2;")}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Execute(context.Background(), req, id, 1); !errors.Is(err, security.ErrConflict) || f.Snapshot().Executes != 0 {
		t.Fatal("changed SQL passed the claim CAS")
	}
	record, _ := s.Migration(context.Background(), req, id)
	if record.Status != migration.DraftStatus || record.Revision != 2 {
		t.Fatal("new draft revision was overwritten")
	}
}

func TestPendingExecutionBlocksNextMigration(t *testing.T) {
	s, f, _ := setup(t)
	first := draft(t, s)
	second := draft(t, s)
	f.CommitError = security.ErrUnavailable
	if _, err := s.Execute(context.Background(), req, first, 1); !errors.Is(err, migration.ErrExecutionUncertain) {
		t.Fatal("expected pending")
	}
	before := f.Snapshot().Executes
	if _, err := s.Execute(context.Background(), req, second, 1); err == nil || f.Snapshot().Executes != before {
		t.Fatal("another migration overtook pending execution")
	}
}
func TestMissingDependenciesAndBadHistoryQuery(t *testing.T) {
	if s, err := migration.NewService(migration.Config{}); s != nil || !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("empty config accepted")
	}
	if r, err := migration.NewReconciler(migration.ReconcilerConfig{}); r != nil || err == nil {
		t.Fatal("empty reconciler accepted")
	}
	s, _, _ := setup(t)
	for _, q := range []migration.HistoryQuery{{Page: -1}, {Limit: 201}, {Page: 20002, Limit: 50}} {
		if _, err := s.History(context.Background(), req, q); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("invalid pagination accepted")
		}
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := s.History(ctx, req, migration.HistoryQuery{}); err == nil {
		t.Fatal("expired request accepted")
	}
}
