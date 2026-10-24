package migration

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	DefaultReconcileInterval = 30 * time.Second
	reconcileBatch           = 50
)

// ReconcileWorker memindai migration pending secara berkala dan menyerahkannya ke Reconciler.
// Worker tidak pernah menjalankan ulang SQL; satu target yang gagal hanya dilewati sampai putaran berikutnya.
type ReconcileWorker struct {
	pending    PendingRepository
	reconciler interface {
		Reconcile(context.Context, security.ID, security.ID) (ExecutionResult, error)
	}
	interval time.Duration
}

func NewReconcileWorker(pending PendingRepository, reconciler *Reconciler, interval time.Duration) (*ReconcileWorker, error) {
	if missing(pending) || reconciler == nil || interval <= 0 {
		return nil, security.ErrInvalidInput
	}
	return &ReconcileWorker{pending: pending, reconciler: reconciler, interval: interval}, nil
}

// Sweep menjalankan satu putaran dan mengembalikan jumlah migration yang statusnya kini pasti.
func (w *ReconcileWorker) Sweep(ctx context.Context) int {
	resolved := 0
	var after security.ID
	for ctx.Err() == nil {
		keys, err := w.pending.Pending(ctx, PendingQuery{After: after, Limit: reconcileBatch})
		if err != nil || len(keys) == 0 {
			return resolved
		}
		for _, key := range keys {
			result, err := w.reconciler.Reconcile(ctx, key.DatabaseID, key.MigrationID)
			if err == nil && result.Status != Pending {
				resolved++
			}
			after = key.MigrationID
		}
		if len(keys) < reconcileBatch {
			return resolved
		}
	}
	return resolved
}

func (w *ReconcileWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.Sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Sweep(ctx)
		}
	}
}
