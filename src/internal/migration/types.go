package migration

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	MaxTitleRunes          = 120
	MaxSQLBytes            = 512 << 10
	DefaultLimit           = 50
	MaxLimit               = 200
	MaxOffset        int64 = 1_000_000
	ExecutionTimeout       = 20 * time.Second
	CleanupTimeout         = 3 * time.Second
)

type Status string

const (
	DraftStatus Status = "draft"
	Pending     Status = "pending"
	Applied     Status = "applied"
	Failed      Status = "failed"
)

type Failure string

const (
	FailureNone        Failure = ""
	FailureSQL         Failure = "sql_error"
	FailureConnection  Failure = "connection_error"
	FailureInterrupted Failure = "interrupted"
)

func (f Failure) Message() string {
	switch f {
	case FailureSQL:
		return "SQL gagal dijalankan; perubahan sudah di-rollback."
	case FailureConnection:
		return "Transaksi tidak dapat dimulai; perubahan tidak dijalankan."
	case FailureInterrupted:
		return "Eksekusi terhenti; tidak ada perubahan yang di-commit."
	default:
		return ""
	}
}

type Request struct {
	Principal  security.Principal
	DatabaseID security.ID
}
type Draft struct {
	Title string
	SQL   security.Secret
}
type Summary struct {
	ID         security.ID
	DatabaseID security.ID
	Title      string
	Status     Status
	Version    int64
	ActorID    security.ID
	Actor      string
	ExecutedAt time.Time
	UpdatedAt  time.Time
}
type Migration struct {
	Summary
	SQL         security.Secret
	Revision    uint64
	ExecutionID security.ID
	Checksum    [32]byte
	Failure     Failure
}
type HistoryQuery struct{ Page, Limit int }
type PendingQuery struct {
	After security.ID
	Limit int
}
type HistoryPage struct {
	Items       []Summary
	Page, Limit int
	HasNext     bool
}
type HistoryOptions struct {
	Offset     int64
	FetchLimit int
}
type ExecutionResult struct {
	MigrationID, ExecutionID security.ID
	Status                   Status
	Version                  int64
	Failure                  Failure
}
type ExecutionKey struct {
	DatabaseID, MigrationID, ExecutionID security.ID
	Version                              int64
	Checksum                             [32]byte
}
type Claim struct {
	MigrationID, ExecutionID security.ID
	Revision                 uint64
	Checksum                 [32]byte
	Actor                    string
	At                       time.Time
}
type Receipt struct{ Key ExecutionKey }
type ReceiptLookup struct {
	Receipt   *Receipt
	Quiescent bool
}

type Application interface {
	History(context.Context, Request, HistoryQuery) (HistoryPage, error)
	Migration(context.Context, Request, security.ID) (Migration, error)
	CreateDraft(context.Context, Request, Draft) (security.ID, error)
	UpdateDraft(context.Context, Request, security.ID, Draft, uint64) error
	Execute(context.Context, Request, security.ID, uint64) (ExecutionResult, error)
}
