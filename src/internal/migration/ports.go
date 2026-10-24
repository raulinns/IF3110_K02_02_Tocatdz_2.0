package migration

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Mutasi wajib memeriksa epoch akun dalam transaksi yang sama dengan CAS draft atau ledger.
type Repository interface {
	History(context.Context, security.ID, HistoryOptions) ([]Summary, error)
	Find(context.Context, security.ID, security.ID) (Migration, error)
	Create(context.Context, Request, Migration) error
	Update(context.Context, Request, security.ID, uint64, Draft, time.Time) error
	Reserve(context.Context, Request, Claim) (Migration, error)
	Finish(context.Context, ExecutionKey, Status, Failure, time.Time) error
}
type Accounts interface {
	FindAccount(context.Context, security.ID) (security.Account, error)
}

// Worker membaca pending per batch berdasarkan ID agar satu target bermasalah tidak menghambat seluruh antrean.
type PendingRepository interface {
	Pending(context.Context, PendingQuery) ([]ExecutionKey, error)
}
type IDs interface{ GenerateID() (security.ID, error) }
type Validator interface {
	Validate(context.Context, security.Secret) error
}

// Lock harus lintas proses, memakai identitas target yang sama untuk alias koneksi, dan menunggu sampai callback selesai.
type Coordinator interface {
	WithDatabaseLock(context.Context, security.ID, func(context.Context, LockedTarget) error) error
}
type LockedTarget interface {
	Begin(context.Context) (Transaction, error)
	LookupReceipt(context.Context, ExecutionKey) (ReceiptLookup, error)
}
type Transaction interface {
	Execute(context.Context, security.Secret) error
	WriteReceipt(context.Context, Receipt) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
