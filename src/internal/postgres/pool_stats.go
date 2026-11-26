package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolStats adalah ringkasan pool koneksi; AcquireWait dan Acquires bersifat kumulatif.
type PoolStats struct {
	Active, Idle int32
	Acquires     int64
	AcquireWait  time.Duration
}

func statsOf(pool *pgxpool.Pool) PoolStats {
	stat := pool.Stat()
	return PoolStats{Active: stat.AcquiredConns(), Idle: stat.IdleConns(), Acquires: stat.AcquireCount(), AcquireWait: stat.AcquireDuration()}
}

func PlatformPoolStats(pool *pgxpool.Pool) PoolStats { return statsOf(pool) }

// PoolStats menjumlahkan seluruh pool database target PostgreSQL yang sedang terbuka.
func (t *Targets) PoolStats() PoolStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	var total PoolStats
	for _, pool := range t.pools {
		stats := statsOf(pool)
		total.Active += stats.Active
		total.Idle += stats.Idle
		total.Acquires += stats.Acquires
		total.AcquireWait += stats.AcquireWait
	}
	return total
}
