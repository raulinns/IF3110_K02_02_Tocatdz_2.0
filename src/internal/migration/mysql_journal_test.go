package migration_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/migrationtest"
)

type journalFixture struct {
	*migrationtest.Fixture
	mu      sync.Mutex
	started map[security.ID]migration.Receipt
}

func newJournalFixture() *journalFixture {
	return &journalFixture{
		Fixture: migrationtest.New(),
		started: make(map[security.ID]migration.Receipt),
	}
}

func (f *journalFixture) WithDatabaseLock(ctx context.Context, id security.ID, run func(context.Context, migration.LockedTarget) error) error {
	return f.Fixture.WithDatabaseLock(ctx, id, func(locked context.Context, _ migration.LockedTarget) error {
		return run(locked, f)
	})
}

func (f *journalFixture) StartReceipt(ctx context.Context, receipt migration.Receipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started[receipt.Key.ExecutionID] = receipt
	return nil
}

func (f *journalFixture) LookupReceipt(ctx context.Context, key migration.ExecutionKey) (migration.ReceiptLookup, error) {
	lookup, err := f.Fixture.LookupReceipt(ctx, key)
	if err != nil {
		return lookup, err
	}
	f.mu.Lock()
	_, lookup.Started = f.started[key.ExecutionID]
	f.mu.Unlock()
	return lookup, nil
}

func (f *journalFixture) hasStarted(id security.ID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.started[id]
	return ok
}

func setupJournaled(t *testing.T) (*migration.Service, *journalFixture) {
	t.Helper()
	fixture := newJournalFixture()
	auth := &authorization{account: security.Account{ID: user, Username: "budi", Active: true, AuthVersion: 1}}
	fixture.Check = func(ctx context.Context, request migration.Request) error {
		return auth.RequireDatabase(ctx, request.Principal, request.DatabaseID, security.ActionExecuteMigration)
	}
	service, err := migration.NewService(migration.Config{
		Repository:  fixture,
		Coordinator: fixture,
		Validator:   migration.NewPostgreSQLValidator(),
		Authorizer:  auth,
		Accounts:    auth,
		IDs:         security.NewTokenGenerator(),
		Clock:       security.SystemClock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, fixture
}

func TestJournaledTargetKeepsFailedDDLUncertain(t *testing.T) {
	service, fixture := setupJournaled(t)
	id := draft(t, service)
	fixture.SQLError = errors.New("implicit commit may already have happened")

	result, err := service.Execute(context.Background(), req, id, 1)
	if !errors.Is(err, migration.ErrExecutionUncertain) || result.Status != migration.Pending {
		t.Fatalf("journaled DDL failure must remain pending, got result=%+v err=%v", result, err)
	}
	if !fixture.hasStarted(result.ExecutionID) {
		t.Fatal("execution intent was not persisted before SQL dispatch")
	}
	if fixture.Snapshot().Finishes != 0 {
		t.Fatal("uncertain DDL execution was finalized as failed")
	}
}

func TestReconcileKeepsStartedReceiptPending(t *testing.T) {
	service, fixture := setupJournaled(t)
	id := draft(t, service)
	fixture.SQLError = errors.New("connection dropped after DDL")

	if _, err := service.Execute(context.Background(), req, id, 1); !errors.Is(err, migration.ErrExecutionUncertain) {
		t.Fatal("expected uncertain execution")
	}
	reconciler, err := migration.NewReconciler(migration.ReconcilerConfig{
		Repository: fixture, Coordinator: fixture, Clock: security.SystemClock{},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := reconciler.Reconcile(context.Background(), db, id)
	if !errors.Is(err, migration.ErrExecutionUncertain) || result.Status != migration.Pending {
		t.Fatalf("started receipt must not be treated as a confirmed rollback, got result=%+v err=%v", result, err)
	}
}
