package migration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/migrationtest"
)

type planFixture struct {
	*migrationtest.Fixture
	raw   []byte
	query migration.PlanQuery
	calls int
	after func()
}

func (f *planFixture) WithPlanLock(ctx context.Context, id security.ID, run func(context.Context, migration.PlanTarget) error) error {
	return f.WithDatabaseLock(ctx, id, func(ctx context.Context, _ migration.LockedTarget) error { return run(ctx, f) })
}
func (f *planFixture) Explain(_ context.Context, query migration.PlanQuery) ([]byte, error) {
	f.query = query
	f.calls++
	if f.after != nil {
		f.after()
	}
	return f.raw, nil
}

func TestPlanServiceEnforcesAccessRevisionAndKeepsDraft(t *testing.T) {
	_, store, auth := setup(t)
	f := &planFixture{Fixture: store, raw: []byte(`[{"Plan":{"Node Type":"Result","Total Cost":1,"Plan Rows":1}}]`)}
	s, err := migration.NewService(migration.Config{Repository: store, Coordinator: f, Validator: migration.NewPostgreSQLValidator(), Authorizer: auth, Accounts: auth, IDs: security.NewTokenGenerator(), Clock: security.SystemClock{}})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateDraft(context.Background(), req, migration.Draft{Title: "Plan", SQL: security.NewSecret("SELECT 1;")})
	if err != nil {
		t.Fatal(err)
	}
	options := migration.PlanOptions{Revision: 1, Statement: 1}
	result, err := s.Analyze(context.Background(), req, id, options)
	if err != nil || result.Revision != 1 || f.calls != 1 {
		t.Fatalf("analysis failed: %v", err)
	}
	record, _ := s.Migration(context.Background(), req, id)
	snapshot := store.Snapshot()
	if record.Status != migration.DraftStatus || record.Version != 0 || record.Revision != 1 || snapshot.Begins != 0 || snapshot.Executes != 0 {
		t.Fatal("analysis changed migration history or executed migration")
	}
	if _, err := s.Analyze(context.Background(), req, id, migration.PlanOptions{Revision: 2, Statement: 1}); !errors.Is(err, security.ErrConflict) || f.calls != 1 {
		t.Fatal("stale revision reached target")
	}
	other := req
	other.DatabaseID = otherDB
	if _, err := s.Analyze(context.Background(), other, id, options); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("cross-database plan allowed")
	}
	auth.access.Store(1)
	if _, err := s.Analyze(context.Background(), req, id, options); !errors.Is(err, security.ErrForbidden) || f.calls != 1 {
		t.Fatal("viewer can analyze")
	}
	auth.access.Store(0)
	f.after = func() { auth.denied.Store(true) }
	if _, err := s.Analyze(context.Background(), req, id, options); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("revoked user received plan")
	}
}

func TestPlanServiceRejectsResultFromChangedDraft(t *testing.T) {
	_, store, auth := setup(t)
	f := &planFixture{Fixture: store, raw: []byte(`[{"Plan":{"Node Type":"Result","Total Cost":1,"Plan Rows":1}}]`)}
	s, err := migration.NewService(migration.Config{Repository: store, Coordinator: f, Validator: migration.NewPostgreSQLValidator(), Authorizer: auth, Accounts: auth, IDs: security.NewTokenGenerator(), Clock: security.SystemClock{}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := s.CreateDraft(context.Background(), req, migration.Draft{Title: "Plan", SQL: security.NewSecret("SELECT 1;")})
	f.after = func() {
		if err := s.UpdateDraft(context.Background(), req, id, migration.Draft{Title: "Changed", SQL: security.NewSecret("SELECT 2;")}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Analyze(context.Background(), req, id, migration.PlanOptions{Revision: 1, Statement: 1}); !errors.Is(err, security.ErrConflict) {
		t.Fatal("changed draft received stale plan")
	}
}
