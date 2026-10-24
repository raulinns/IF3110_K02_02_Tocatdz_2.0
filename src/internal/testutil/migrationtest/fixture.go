package migrationtest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Fixture ini hanya untuk test domain dan HTTP, bukan adapter database produksi.
type Fixture struct {
	BeforeReserve      func()
	mu                 sync.Mutex
	lock               chan struct{}
	records            map[security.ID]migration.Migration
	versions           map[security.ID]int64
	receipts           map[security.ID]migration.Receipt
	Check              func(context.Context, migration.Request) error
	AfterLock          func()
	BeforeSQL          func()
	AfterSQL           func()
	BeginError         error
	SQLError           error
	ReceiptError       error
	CommitError        error
	RollbackError      error
	FinishError        error
	CommitDespiteError bool
	ProbeNotQuiescent  bool
	ProbeMismatch      bool
	Counts             Counts
}
type Counts struct{ Begins, Executes, Commits, Rollbacks, Finishes int }

func New() *Fixture {
	f := &Fixture{lock: make(chan struct{}, 1), records: map[security.ID]migration.Migration{}, versions: map[security.ID]int64{}, receipts: map[security.ID]migration.Receipt{}}
	f.lock <- struct{}{}
	return f
}
func (f *Fixture) Snapshot() Counts { f.mu.Lock(); defer f.mu.Unlock(); return f.Counts }
func (f *Fixture) Seed(record migration.Migration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[record.ID] = record
}
func (f *Fixture) Records() []migration.Migration {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := []migration.Migration{}
	for _, record := range f.records {
		result = append(result, record)
	}
	return result
}
func (f *Fixture) History(ctx context.Context, db security.ID, opts migration.HistoryOptions) ([]migration.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := []migration.Summary{}
	for _, record := range f.records {
		if record.DatabaseID == db {
			result = append(result, record.Summary)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if (result[i].Status == migration.DraftStatus) != (result[j].Status == migration.DraftStatus) {
			return result[i].Status == migration.DraftStatus
		}
		if result[i].Version != result[j].Version {
			return result[i].Version > result[j].Version
		}
		if !result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].UpdatedAt.After(result[j].UpdatedAt)
		}
		return result[i].ID < result[j].ID
	})
	start := min(int64(len(result)), opts.Offset)
	end := min(int64(len(result)), start+int64(opts.FetchLimit))
	return result[start:end], nil
}
func (f *Fixture) Find(ctx context.Context, db, id security.ID) (migration.Migration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return migration.Migration{}, err
	}
	record, ok := f.records[id]
	if !ok || record.DatabaseID != db {
		return migration.Migration{}, security.ErrNotFound
	}
	return record, nil
}
func (f *Fixture) check(ctx context.Context, req migration.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.Check != nil {
		return f.Check(ctx, req)
	}
	return nil
}
func (f *Fixture) Create(ctx context.Context, req migration.Request, record migration.Migration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(ctx, req); err != nil {
		return err
	}
	if _, ok := f.records[record.ID]; ok {
		return security.ErrConflict
	}
	f.records[record.ID] = record
	return nil
}
func (f *Fixture) Update(ctx context.Context, req migration.Request, id security.ID, revision uint64, draft migration.Draft, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(ctx, req); err != nil {
		return err
	}
	record, ok := f.records[id]
	if !ok || record.DatabaseID != req.DatabaseID {
		return security.ErrNotFound
	}
	if record.Status != migration.DraftStatus || record.Revision != revision {
		return security.ErrConflict
	}
	record.Title = draft.Title
	record.SQL = draft.SQL
	record.UpdatedAt = at
	record.Revision++
	f.records[id] = record
	return nil
}
func (f *Fixture) Reserve(ctx context.Context, req migration.Request, claim migration.Claim) (migration.Migration, error) {
	if f.BeforeReserve != nil {
		f.BeforeReserve()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(ctx, req); err != nil {
		return migration.Migration{}, err
	}
	record, ok := f.records[claim.MigrationID]
	if !ok || record.DatabaseID != req.DatabaseID {
		return migration.Migration{}, security.ErrNotFound
	}
	if record.Status != migration.DraftStatus || record.Revision != claim.Revision || migration.Checksum(record.SQL) != claim.Checksum {
		return migration.Migration{}, security.ErrConflict
	}
	for _, other := range f.records {
		if other.DatabaseID == req.DatabaseID && other.Status == migration.Pending {
			return migration.Migration{}, migration.ErrExecutionUncertain
		}
	}
	f.versions[req.DatabaseID]++
	record.Version = f.versions[req.DatabaseID]
	record.Status = migration.Pending
	record.ExecutionID = claim.ExecutionID
	record.Checksum = claim.Checksum
	record.ActorID = req.Principal.UserID
	record.Actor = claim.Actor
	record.ExecutedAt = claim.At
	record.UpdatedAt = claim.At
	record.Revision++
	f.records[record.ID] = record
	return record, nil
}
func (f *Fixture) Finish(ctx context.Context, key migration.ExecutionKey, status migration.Status, failure migration.Failure, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.FinishError != nil {
		return f.FinishError
	}
	record, ok := f.records[key.MigrationID]
	if !ok || record.DatabaseID != key.DatabaseID || record.ExecutionID != key.ExecutionID || record.Checksum != key.Checksum || record.Version != key.Version {
		return security.ErrConflict
	}
	if record.Status != migration.Pending {
		if record.Status == status && record.Failure == failure {
			return nil
		}
		return security.ErrConflict
	}
	record.Status = status
	record.Failure = failure
	record.UpdatedAt = at
	record.Revision++
	f.records[record.ID] = record
	f.Counts.Finishes++
	return nil
}
func (f *Fixture) WithDatabaseLock(ctx context.Context, _ security.ID, run func(context.Context, migration.LockedTarget) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.lock:
	}
	defer func() { f.lock <- struct{}{} }()
	if f.AfterLock != nil {
		f.AfterLock()
	}
	return run(ctx, f)
}
func (f *Fixture) Begin(ctx context.Context) (migration.Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Counts.Begins++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.BeginError != nil {
		return nil, f.BeginError
	}
	return &transaction{fixture: f}, nil
}
func (f *Fixture) LookupReceipt(ctx context.Context, key migration.ExecutionKey) (migration.ReceiptLookup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return migration.ReceiptLookup{}, err
	}
	result := migration.ReceiptLookup{Quiescent: !f.ProbeNotQuiescent}
	if receipt, ok := f.receipts[key.ExecutionID]; ok {
		if f.ProbeMismatch {
			receipt.Key.Checksum = [32]byte{}
		}
		result.Receipt = &receipt
	}
	return result, nil
}

type transaction struct {
	fixture *Fixture
	receipt *migration.Receipt
	closed  bool
}

func (tx *transaction) Execute(ctx context.Context, _ security.Secret) error {
	f := tx.fixture
	if f.BeforeSQL != nil {
		f.BeforeSQL()
	}
	f.mu.Lock()
	f.Counts.Executes++
	err := f.SQLError
	f.mu.Unlock()
	if f.AfterSQL != nil {
		f.AfterSQL()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (tx *transaction) WriteReceipt(ctx context.Context, receipt migration.Receipt) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if tx.fixture.ReceiptError != nil {
		return tx.fixture.ReceiptError
	}
	tx.receipt = &receipt
	return nil
}
func (tx *transaction) Commit(ctx context.Context) error {
	f := tx.fixture
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Counts.Commits++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if f.CommitError == nil || f.CommitDespiteError {
		if tx.receipt != nil {
			f.receipts[tx.receipt.Key.ExecutionID] = *tx.receipt
		}
		tx.closed = true
	}
	return f.CommitError
}
func (tx *transaction) Rollback(ctx context.Context) error {
	f := tx.fixture
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Counts.Rollbacks++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if f.RollbackError != nil {
		return f.RollbackError
	}
	tx.closed = true
	return nil
}
