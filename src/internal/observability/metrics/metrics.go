// Package metrics mengekspos metrik Prometheus untuk queue, migration, dan pool koneksi.
// Label sengaja dibatasi (status, kind, mode, pool); ID user, job, dan SQL tidak pernah menjadi label.
package metrics

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
)

const namespace = "tocatdz"

var jobStatuses = []migration.JobStatus{migration.JobQueued, migration.JobRunning, migration.JobSucceeded, migration.JobFailed,
	migration.JobPartial, migration.JobUncertain, migration.JobPartiallyReverted}

var jobKinds = []migration.ChangeKind{migration.ChangeApply, migration.ChangeRollback, migration.ChangeForward,
	migration.ChangeResume, migration.ChangeRevertBatches}

var jobModes = []migration.ExecutionMode{migration.ModeNormal, migration.ModeBatch}

// JobCounter memberi jumlah job per status saat metrik di-scrape.
type JobCounter interface {
	JobStatusCounts(ctx context.Context) (map[migration.JobStatus]int64, error)
}

type Registry struct {
	registry      *prometheus.Registry
	queueWait     prometheus.Histogram
	lockWait      prometheus.Histogram
	duration      *prometheus.HistogramVec
	finished      *prometheus.CounterVec
	retries       *prometheus.CounterVec
	batches, rows *prometheus.CounterVec
	pools         *poolCollector
}

func New() *Registry {
	r := &Registry{registry: prometheus.NewRegistry()}
	buckets := []float64{0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 300, 900}
	r.queueWait = prometheus.NewHistogram(prometheus.HistogramOpts{Namespace: namespace, Name: "queue_wait_seconds",
		Help: "Waktu job menunggu di queue sebelum diambil worker.", Buckets: buckets})
	r.lockWait = prometheus.NewHistogram(prometheus.HistogramOpts{Namespace: namespace, Name: "lock_wait_seconds",
		Help: "Waktu worker menunggu lock database target.", Buckets: buckets})
	r.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Name: "migration_duration_seconds",
		Help: "Durasi eksekusi migration di target, tanpa waktu tunggu.", Buckets: buckets}, []string{"mode", "kind"})
	r.finished = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "jobs_finished_total",
		Help: "Job yang selesai diproses menurut status akhir."}, []string{"status", "kind", "mode"})
	r.retries = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "job_retries_total",
		Help: "Retry yang dijadwalkan untuk kegagalan sementara."}, []string{"kind"})
	r.batches = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "batches_processed_total",
		Help: "Batch yang ter-commit di target."}, []string{"direction"})
	r.rows = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "rows_processed_total",
		Help: "Row yang diubah oleh batch yang ter-commit."}, []string{"direction"})
	r.pools = &poolCollector{
		connections: prometheus.NewDesc(namespace+"_db_pool_connections", "Koneksi pool menurut state.", []string{"pool", "state"}, nil),
		wait:        prometheus.NewDesc(namespace+"_db_pool_acquire_wait_seconds_total", "Total waktu menunggu koneksi dari pool.", []string{"pool"}, nil),
		acquires:    prometheus.NewDesc(namespace+"_db_pool_acquires_total", "Jumlah pengambilan koneksi dari pool.", []string{"pool"}, nil),
		sources:     map[string]PoolSource{}}
	r.registry.MustRegister(r.pools, r.queueWait, r.lockWait, r.duration, r.finished, r.retries, r.batches, r.rows,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	// Seri awal bernilai nol agar grafik dan rate() tidak kosong sebelum ada job.
	for _, kind := range jobKinds {
		r.retries.WithLabelValues(string(kind))
		for _, mode := range jobModes {
			r.duration.WithLabelValues(string(mode), string(kind))
			for _, status := range jobStatuses {
				r.finished.WithLabelValues(string(status), string(kind), string(mode))
			}
		}
	}
	for _, direction := range []string{"up", "down"} {
		r.batches.WithLabelValues(direction)
		r.rows.WithLabelValues(direction)
	}
	return r
}

func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

// WatchJobs menambahkan gauge tocatdz_jobs{status}; kueri dijalankan setiap scrape dengan batas waktu singkat.
func (r *Registry) WatchJobs(source JobCounter) {
	r.registry.MustRegister(&jobCollector{source: source,
		desc: prometheus.NewDesc(namespace+"_jobs", "Jumlah job per status saat ini.", []string{"status"}, nil)})
}

type jobCollector struct {
	source JobCounter
	desc   *prometheus.Desc
}

func (c *jobCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *jobCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	counts, err := c.source.JobStatusCounts(ctx)
	if err != nil {
		return
	}
	for _, status := range jobStatuses {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(counts[status]), string(status))
	}
}

// PoolSource membaca statistik pool koneksi saat scrape.
type PoolSource func() postgres.PoolStats

// WatchPool menambahkan koneksi aktif dan idle serta waktu tunggu koneksi untuk satu pool.
func (r *Registry) WatchPool(name string, source PoolSource) {
	r.pools.mu.Lock()
	defer r.pools.mu.Unlock()
	r.pools.sources[name] = source
}

type poolCollector struct {
	mu                          sync.Mutex
	sources                     map[string]PoolSource
	connections, wait, acquires *prometheus.Desc
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.wait
	ch <- c.acquires
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, source := range c.sources {
		stats := source()
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.Active), name, "active")
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.Idle), name, "idle")
		ch <- prometheus.MustNewConstMetric(c.wait, prometheus.CounterValue, stats.AcquireWait.Seconds(), name)
		ch <- prometheus.MustNewConstMetric(c.acquires, prometheus.CounterValue, float64(stats.Acquires), name)
	}
}

// Metode berikut memenuhi worker.Observer.

func (r *Registry) QueueWait(d time.Duration) { r.queueWait.Observe(d.Seconds()) }
func (r *Registry) LockWait(d time.Duration)  { r.lockWait.Observe(d.Seconds()) }

func (r *Registry) RunFinished(kind migration.ChangeKind, mode migration.ExecutionMode, d time.Duration) {
	r.duration.WithLabelValues(string(mode), string(kind)).Observe(d.Seconds())
}

func (r *Registry) JobFinished(status migration.JobStatus, kind migration.ChangeKind, mode migration.ExecutionMode) {
	r.finished.WithLabelValues(string(status), string(kind), string(mode)).Inc()
}

func (r *Registry) Retry(kind migration.ChangeKind) { r.retries.WithLabelValues(string(kind)).Inc() }

// Progress membungkus ProgressSink agar batch dan row yang ter-commit ikut dihitung.
func (r *Registry) Progress(next migration.ProgressSink) migration.ProgressSink {
	return progress{registry: r, next: next}
}

type progress struct {
	registry *Registry
	next     migration.ProgressSink
}

func (p progress) BatchCommitted(ctx context.Context, checkpoint migration.BatchCheckpoint) error {
	if checkpoint.Rows > 0 {
		p.registry.batches.WithLabelValues("up").Inc()
		p.registry.rows.WithLabelValues("up").Add(float64(checkpoint.Rows))
	}
	return p.next.BatchCommitted(ctx, checkpoint)
}

func (p progress) BatchReverted(ctx context.Context, checkpoint migration.BatchCheckpoint) error {
	if checkpoint.Rows > 0 {
		p.registry.batches.WithLabelValues("down").Inc()
		p.registry.rows.WithLabelValues("down").Add(float64(checkpoint.Rows))
	}
	return p.next.BatchReverted(ctx, checkpoint)
}
