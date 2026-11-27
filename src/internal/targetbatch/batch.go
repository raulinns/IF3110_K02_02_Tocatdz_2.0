// Package targetbatch berisi kapabilitas batch pada koneksi target yang sedang dikunci.
package targetbatch

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Direction string

const (
	Up   Direction = "up"
	Down Direction = "down"
)

// Checkpoint ditulis dalam transaksi yang sama dengan UPDATE batch. Keys adalah key yang diubah.
type Checkpoint struct {
	ExecutionID security.ID
	Direction   Direction
	Batch       int64
	From        *int64
	To          int64
	ScopeMax    int64
	Rows        int64
	Keys        []int64
	Final       bool
	RecordedAt  time.Time
}

type KeyInfo struct {
	Exists, Integer, NotNull, Unique bool
}

type Query struct {
	AfterBatch, BeforeBatch int64 // eksklusif; 0 berarti tanpa batas
	Limit                   int
	Descending              bool
}

type Target interface {
	migration.LockedTarget
	BatchSupported(ctx context.Context) error
	KeyColumn(ctx context.Context, schema, table, column string) (KeyInfo, error)
	ScopeMax(ctx context.Context, schema, table, column string) (*int64, error)
	Checkpoints(ctx context.Context, executionID security.ID, direction Direction, query Query) ([]Checkpoint, error)
	BeginBatch(ctx context.Context) (Tx, error)
}

type Tx interface {
	UpdateKeys(ctx context.Context, sql string, args ...any) ([]int64, error)
	WriteCheckpoint(ctx context.Context, checkpoint Checkpoint) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}
