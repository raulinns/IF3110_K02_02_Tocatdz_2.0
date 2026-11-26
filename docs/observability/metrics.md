# Observability: metrik, dashboard, dan stack

Dokumen ini menjelaskan bonus observability (lane 13524044): metrik Prometheus untuk queue, migration, dan pool
koneksi, dashboard Grafana, serta stack OpenTelemetry Collector. Skema log dan trace wajib ada di
[log-schema.md](log-schema.md).

## Menjalankan

```bash
docker compose --profile observability up -d --build
```

| Layanan | Alamat | Fungsi |
|---|---|---|
| Grafana | http://localhost:3000 | Dashboard "Tocatdz: Queue, Migration, dan Koneksi" (anonim hanya melihat, admin: `admin`/`admin`) |
| Prometheus | http://localhost:9090 | Mengambil `/metrics` dari server dan worker setiap 5 detik |
| OTel Collector | `:4317` (gRPC), `:4318` (HTTP) | Menerima OTLP, log diteruskan ke Loki dan trace ke Tempo |
| Loki, Tempo | internal | Penyimpanan log dan trace |

Tanpa Docker, jalankan server atau worker dengan `METRICS_ADDR=:9100` lalu buka `http://localhost:9100/metrics`.
Listener metrik sengaja terpisah dari API publik agar `/metrics` tidak ikut terbuka ke pengguna aplikasi.
Kosongkan `METRICS_ADDR` untuk mematikannya.

## Metrik

Label terbatas pada `status`, `kind`, `mode`, `direction`, dan `pool`. ID user, ID job, dan SQL tidak pernah
menjadi label.

| Metrik | Tipe | Arti |
|---|---|---|
| `tocatdz_jobs{status}` | gauge | Jumlah job per status (queued, running, succeeded, failed, partial, uncertain, partially_reverted) |
| `tocatdz_jobs_finished_total{status,kind,mode}` | counter | Job yang selesai diproses |
| `tocatdz_job_retries_total{kind}` | counter | Retry yang dijadwalkan |
| `tocatdz_queue_wait_seconds` | histogram | Waktu job siap dijalankan sampai diambil worker |
| `tocatdz_lock_wait_seconds` | histogram | Waktu menunggu lock database target |
| `tocatdz_migration_duration_seconds{mode,kind}` | histogram | Durasi eksekusi di target |
| `tocatdz_batches_processed_total{direction}` | counter | Batch ter-commit (`up`) atau dibatalkan (`down`) |
| `tocatdz_rows_processed_total{direction}` | counter | Row pada batch tersebut |
| `tocatdz_db_pool_connections{pool,state}` | gauge | Koneksi `active` dan `idle`; pool `platform` dan `target` |
| `tocatdz_db_pool_acquire_wait_seconds_total{pool}` | counter | Total waktu menunggu koneksi |
| `tocatdz_db_pool_acquires_total{pool}` | counter | Jumlah pengambilan koneksi |

Waktu tunggu koneksi rata-rata dihitung dari `rate(..._wait_seconds_total) / rate(..._acquires_total)`.
Pool `target` menjumlahkan seluruh database target PostgreSQL yang sedang terbuka; target MySQL memakai
`database/sql` dan belum dihitung. Metrik proses (`go_*`, `process_*`) ikut diekspos.

Metrik worker (`queue_wait`, `lock_wait`, durasi, batch, retry, job selesai) hanya terisi pada proses yang
menjalankan worker. Gauge `tocatdz_jobs` dan metrik pool diekspos oleh server maupun worker, sehingga dashboard
memakai `max by (status)` agar tidak menghitung ganda.

## Dashboard

`deploy/grafana/dashboards/tocatdz.json` dimuat otomatis lewat `deploy/grafana/provisioning/`. Isinya 20 panel:
job per status dan job selesai, waktu tunggu queue dan lock (p50, p95), durasi migration (p50, p95 per mode),
batch dan row per detik, retry, koneksi aktif dan idle, waktu tunggu koneksi, serta panel log Loki.

## Contoh hasil

Stack dijalankan dari repository bersih (PostgreSQL 17, dua database target `demo` dan `gudang`) dengan delapan
job: lima migration normal, satu migration batch 300.003 row (`batch_size` 10.000), dan tiga yang gagal
(dua ditolak saat revalidasi, satu SQL error `42P01`). Dua database diproses bersamaan. Hasil kueri Prometheus:

| Kueri | Hasil |
|---|---|
| `max by (status) (tocatdz_jobs)` | succeeded 5, failed 3, lainnya 0 |
| `sum by (status,kind,mode) (tocatdz_jobs_finished_total) > 0` | apply/batch/succeeded 1, apply/normal/succeeded 4, apply/normal/failed 3 |
| `sum by (direction) (tocatdz_rows_processed_total)` | up 300.003 |
| `sum(tocatdz_queue_wait_seconds_count)` | 8 |
| `sum(tocatdz_lock_wait_seconds_count)` | 8 |
| Rata-rata durasi mode batch | 4,15 detik |
| `sum by (pool,state) (tocatdz_db_pool_connections)` | platform aktif 1 / idle 3, target aktif 0 / idle 2 |
| `sum by (pool) (tocatdz_db_pool_acquires_total)` | platform 279, target 8 |

Seluruh 21 kueri panel dijalankan terhadap Prometheus dan semuanya berhasil serta berisi data. Dua job
`pg_sleep` ditolak saat revalidasi oleh kebijakan SQL, bukan karena kesalahan metrik.

## Korelasi log, trace, dan metrik

Infrastrukturnya sudah siap: Collector menerima OTLP dan meneruskannya, sumber data Loki memiliki
*derived field* `trace_id` yang membuka Tempo, dan Tempo dapat membuka log terkait.

Contoh korelasi `trace_id` dari Tempo ke log di Loki lalu ke lonjakan metrik **belum dapat ditampilkan**.
Paket `observability/logging` dan `observability/tracing` (lane 13524056) sudah memuat instrumentasinya,
tetapi `cmd/server` dan `cmd/worker` belum memasangnya: keduanya masih memakai logger lama dan belum memanggil
`tracing.NewOpenTelemetry` dan `tracing.Install`, sehingga span belum dihasilkan. Pemasangan itu adalah tugas
integrator. Setelah terpasang, jalankan dengan `OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318`
(OTLP/HTTP), lalu lengkapi bagian ini dengan tangkapan layar korelasinya. Log aplikasi ke Loki juga perlu jalur
pengumpulan dari stdout, karena paket tracing hanya mengekspor trace lewat OTLP.
