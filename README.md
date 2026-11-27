# IF3110_K02_02_Tocatdz_2.0

Tugas Besar IF3110 Pengembangan Aplikasi Web: Database Management Platform berbasis Go dengan antarmuka SPA. PostgreSQL digunakan sebagai database metadata platform, sedangkan database target dapat menggunakan PostgreSQL atau MySQL.

Alur yang didukung: login → akses lewat group → pilih database → lihat schema/table/data → buat migration (SQL up dan down, atau irreversible dengan alasan) → ajukan → disetujui leader setiap group yang berhak → antrean dan worker menjalankan job (mode normal atau per batch) → pantau status → riwayat, rollback, dan forward. Platform admin mengelola user, group, koneksi database, dan akses.

Komponen: `src/cmd/server` (API dan SPA), `src/cmd/worker` (antrean job), `web/` (SPA React), dan `loadtest/k6/` (stress test).

Bonus yang dikerjakan: **dukungan DBMS tambahan MySQL**, **schema diff dan migration generation**, serta **query execution plan analysis**. Schema diff dan query execution plan saat ini khusus untuk database target PostgreSQL.

## Prasyarat

- Go 1.27.3 atau lebih baru
- PostgreSQL 17/18 (lewat Docker atau terpasang lokal)
- MySQL 8.4 atau lebih baru jika menggunakan atau menguji database target MySQL

## Menjalankan secara lokal

Perintah Docker dijalankan dari root repository, sedangkan semua perintah `go`
dijalankan dari folder `src/`.

1. **Masuk ke root repository dan jalankan PostgreSQL.**

   ```sh
   docker compose up -d postgres
   docker compose ps
   ```

   Konfigurasi Docker bawaan membuat PostgreSQL di `localhost:5433`, database
   `platform` untuk metadata aplikasi, database `demo` dan `gudang` sebagai
   target contoh, dan user/password lokal `app`/`app`. Tunggu sampai service berstatus
   `healthy` sebelum menjalankan seed.

   Jika memakai PostgreSQL lokal tanpa Docker, buat database metadata dan target
   sendiri, lalu sesuaikan `DATABASE_URL` dan `SEED_TARGET_DATABASE_URL`.

2. **Buat file konfigurasi lokal.**

   Dari root repository:

   ```sh
   cp .env.example .env
   cd src
   go run ./cmd/keygen
   ```

   `cmd/keygen` **hanya mencetak** tiga kunci ke terminal. Salin hasilnya untuk
   mengganti tiga nilai kosong berikut di file `../.env`:

   ```dotenv
   SESSION_ENCRYPTION_KEY=<hasil SESSION_ENCRYPTION_KEY dari keygen>
   CSRF_HMAC_KEY=<hasil CSRF_HMAC_KEY dari keygen>
   CONNECTION_ENCRYPTION_KEY=<hasil CONNECTION_ENCRYPTION_KEY dari keygen>
   ```

   Ketiga nilai harus berbeda dan masing-masing merupakan base64 dari 32 byte
   acak. Isi juga `SEED_ADMIN_PASSWORD`, `SEED_VIEWER_PASSWORD`, dan
   `SEED_EDITOR_PASSWORD`; setiap password minimal 15 karakter. Jangan menambahkan
   `export` di dalam `.env` karena loader aplikasi mengharapkan format
   `NAMA_VARIABEL=nilai`.

   File `.env` sudah diabaikan Git. Jangan commit atau membagikan kunci dan
   password. Jika kunci pernah terkirim ke chat, issue tracker, atau tempat
   publik lain, jalankan `keygen` kembali dan gunakan hasil baru.

3. **Isi data demo.**

   Masih dari folder `src/`:

   ```sh
   go run ./cmd/seed
   ```

   Seed membuat schema platform, akun, group, grant akses, dan tabel contoh pada
   database target. Perintah ini aman dijalankan berulang. Seed memakai
   kredensial pemilik target untuk setup, lalu menyimpan DSN akun SQL terbatas
   yang dibuat otomatis. Akun superuser tidak dipakai untuk menjalankan SQL
   migration.

   Untuk instalasi PostgreSQL lokal, akun setup perlu hak yang cukup untuk
   menyiapkan role target dan extension `pgcrypto` harus tersedia. Gunakan
   database target khusus aplikasi; setup memindahkan kepemilikan objek aplikasi
   milik akun setup ke akun SQL tersebut.

4. **Build SPA, lalu jalankan server dan worker.**

   ```sh
   (cd ../web && npm ci && npm run build:embed)
   go run ./cmd/server
   go run ./cmd/worker
   ```

   `build:embed` menyalin hasil build ke `src/internal/spa/dist` sehingga ikut
   ditanam ke binary server. Tanpa langkah ini server menampilkan halaman
   placeholder. Worker menjalankan job migration dari queue; untuk pengembangan
   cukup satu proses dengan `EMBEDDED_WORKER=true go run ./cmd/server`.

   Buka `http://localhost:8080`. Alamat yang dibuka di browser harus sama persis
   dengan nilai `PUBLIC_ORIGIN`.

   Alternatifnya, seluruh aplikasi dapat dijalankan di Docker:

   ```sh
   docker compose up -d --build
   docker compose run --rm \
     -e SEED_TARGET_DATABASE_URL=postgres://app:app@postgres:5432/demo?sslmode=disable \
     -e SEED_TARGET2_DATABASE_URL=postgres://app:app@postgres:5432/gudang?sslmode=disable \
     server seed
   ```

   Seed dari dalam container memakai host `postgres` agar DSN target yang
   tersimpan bisa dihubungi oleh server dan worker di jaringan Docker.

5. **Hentikan service lokal setelah selesai.**

   Jalankan dari root repository:

   ```sh
   docker compose down
   ```

   Perintah ini mempertahankan data pada volume Docker. Gunakan penghapusan
   volume hanya jika memang ingin mengulang database lokal dari keadaan kosong.

### Troubleshooting konfigurasi awal

Jika `cmd/seed` menampilkan pesan berikut:

```text
SESSION_ENCRYPTION_KEY, CSRF_HMAC_KEY and CONNECTION_ENCRYPTION_KEY must be distinct base64 encodings of 32 random bytes
```

berarti setidaknya satu kunci belum terisi, formatnya bukan base64 32 byte, atau
ada dua kunci yang sama. Pastikan output `cmd/keygen` sudah **disalin ke `.env`**;
sekadar menjalankan `keygen` tidak mengubah environment maupun file. Saat
perintah Go dijalankan dari `src/`, aplikasi mencari `.env` di `src/.env` lalu
`../.env`, sehingga lokasi yang direkomendasikan adalah root repository.

Variabel yang sudah diekspor oleh shell memiliki prioritas lebih tinggi daripada
isi `.env`. Jika `.env` sudah benar tetapi error tetap muncul, hapus nilai lama
dari terminal aktif lalu jalankan seed kembali:

```sh
unset SESSION_ENCRYPTION_KEY CSRF_HMAC_KEY CONNECTION_ENCRYPTION_KEY
go run ./cmd/seed
```

Database target MySQL ditambahkan melalui halaman **Administrasi → Koneksi** dengan memilih DBMS **MySQL**. Connection string mengikuti format driver `go-sql-driver/mysql`, misalnya:

```text
tocatdz:password@tcp(127.0.0.1:3306)/nama_database
```

Database dan akun MySQL disiapkan terlebih dahulu di luar aplikasi. Akun tersebut perlu dapat membaca katalog `information_schema`, membaca data, menjalankan SQL migration yang diizinkan, serta membuat dan memperbarui tabel receipt `_tocatdz_migration_receipts`. Simpan hanya DSN akun khusus target, bukan akun metadata PostgreSQL platform.

Skema database platform diterapkan otomatis saat server atau seed berjalan (`src/internal/postgres/schema/*.sql`).

## Worker, queue, lock, dan pemulihan

Setiap job dijalankan **paling banyak satu kali secara efektif**: antrean boleh mengirim ulang job (*at-least-once*),
tetapi receipt di database target menjamin commit tidak terjadi dua kali.

```text
API execute (202) ── satu transaksi: INSERT jobs + CR approved → queued
        │
worker ClaimJob ── SELECT … FOR UPDATE SKIP LOCKED, attempt+1, lease_token+1, catat job_attempts
        │
WithDatabaseLock ── pg_advisory_lock (session-level) di database target
        │
Revalidator ── gagal → job failed (revalidation_failed), tanpa SQL ke target
        │
StartRunning ── CAS ulang CR/approval/definisi/posisi di bawah guard, simpan execution_plan
        │
Runner ── satu transaksi target: SQL migration + receipt, lalu COMMIT
        │
CompleteCommitted ── satu transaksi platform: executions + posisi + job/CR/attempt
```

| Aturan | Mekanisme |
|---|---|
| Satu database menjalankan satu migration | Claim melewati database yang punya job `running`/`uncertain`. Advisory lock di target menjadi pengaman lintas proses dan alias koneksi. |
| Database berbeda diproses paralel | `WORKER_CONCURRENCY` loop per proses, dan beberapa proses `cmd/worker` boleh berjalan bersamaan. `SKIP LOCKED` membuat worker tidak saling menunggu. |
| Lock berlaku lintas worker dan di antara commit batch | Lock bersifat session-level pada satu koneksi khusus dan tidak lepas saat transaksi batch di-commit. |
| Pesan duplikat tidak menyebabkan double commit | `UNIQUE(change_request_id)` pada `jobs`, lease token yang memagari worker lama, dan receipt per `execution_id` yang diperiksa sebelum SQL dijalankan. |

**Retry otomatis.** Hanya untuk kegagalan sementara: SQLSTATE `40001`, `40P01`, `55P03`, kelas `08`, serta `57P01`–`57P03`, atau target tidak bisa dihubungi
sebelum ada SQL yang dikirim. Jumlah attempt dibatasi `max_attempts`, dengan backoff eksponensial (2 detik, 4 detik, … maksimum 1 menit) dan jitter.
Setiap attempt, termasuk yang gagal, tercatat di `job_attempts`. Error SQL permanen (misalnya `42P01`) langsung menjadi `failed`: transaksi di-rollback,
attempt gagal tetap muncul di Riwayat tanpa commit baru, dan posisi aktif tetap.

**Pemulihan lock ketika worker gagal.**
1. Worker mati saat memegang lock: koneksi ke target terputus dan PostgreSQL otomatis melepas advisory lock session-level milik koneksi itu. Lock tidak pernah tertinggal.
2. Transaksi yang belum di-commit ikut di-rollback oleh PostgreSQL ketika koneksi putus.
3. Lease job kedaluwarsa karena heartbeat berhenti. Loop rekonsiliasi mengklaim ulang job dengan menaikkan `lease_token`, sehingga worker lama tidak bisa lagi mencatat hasil.
4. Rekonsiliasi **tidak menjalankan SQL**. Ia hanya membaca bukti di target di bawah lock: belum ada `execution_plan` → job dijadwalkan ulang; receipt cocok → commit terbukti dan
   dicatat; tidak ada receipt dan target tenang → retry atau `failed`; bukti tidak bisa dipastikan → `uncertain` dan database ditahan (barrier) sampai terbukti.
5. Jika commit target berhasil tetapi pencatatan di platform gagal, job tetap `running` sampai lease habis, lalu langkah 4 menyelesaikannya dari receipt. Commit kedua tidak mungkin terjadi.

**Menjalankan worker.** `cd src && go run ./cmd/worker` memakai `.env` yang sama dengan server. Variabel khusus: `WORKER_ID` (bawaan `hostname-pid`) dan `WORKER_CONCURRENCY`
(bawaan 2, rentang 1–64). Untuk pengembangan, worker bisa berjalan di dalam proses server dengan `EMBEDDED_WORKER=true`. Worker menolak start jika revalidator tidak tersedia.

### Riwayat seluruh attempt

Riwayat mencatat semua attempt, termasuk gagal, partial, uncertain, retry, rollback, dan forward.
Setiap attempt mempunyai ID history sendiri; retry otomatis dapat memakai execution ID yang sama.
SQL yang dikirim ke driver dicatat sebelum dispatch, lalu hasil statement disimpan terpisah agar tetap tersedia
ketika transaksi target dibatalkan. Statement berhasil bukan bukti commit transaksi; hasil yang belum tercatat
ditampilkan sebagai belum diketahui. Snapshot SQL dan attempt yang telah selesai tidak ditimpa.

Hide rollback menyembunyikan pasangan eksekusi committed dan rollback sukses berdasarkan execution ID,
sebelum pagination. Kegagalan dan forward berikutnya tetap terlihat. Status attempt awal tetap tersimpan
ketika rekonsiliasi menambahkan hasil yang sudah terbukti. Data lama tanpa snapshot ditandai tidak tersedia.

Kontrak lengkap tersedia di [OpenAPI](api/openapi.yaml). Endpoint berikut memakai prefix `/api/v1`:

| Endpoint | Fungsi |
|---|---|
| `GET /databases/{database_id}/history` | Daftar seluruh attempt; mendukung `page`, `limit`, dan `hide_rollback`, tanpa badan SQL. |
| `GET /databases/{database_id}/history/{history_id}` | Detail dan snapshot SQL immutable untuk pengguna dengan grant database. |
| `GET /databases/{database_id}/history/{history_id}/statements` | Statement per halaman, hasil, dan SQLSTATE; literal serta parameter sensitif disamarkan. |
| `POST /databases/{database_id}/jobs/{job_id}/retry` | JSON `{}` membuat CR baru untuk job normal gagal yang dapat diulang, dengan approval seluruh group kembali. Memerlukan grant ubah dan CSRF. |

Retry manual memakai SQL asli dan target yang sama; perubahan posisi, permission, checksum, source yang belum pasti,
dan pengajuan ganda diperiksa ulang. Jika SQL perlu diubah, buat migration baru. Pemulihan batch tetap melalui Resume/Revert.
`/executions` dan `/position` tetap memakai bukti commit untuk menentukan posisi linear; attempt gagal tidak menjadi posisi baru.
Schema `0005_attempt_history.sql` diterapkan migrator platform. Build ulang lalu restart server dan worker bersama saat menerapkan versi ini.

### Mode batch

Konfigurasi batch: `schema`, `table`, `key_column` (integer, unik, NOT NULL, tidak boleh diubah oleh SET), dan `batch_size` (100–100.000). `up_sql` dan `down_sql` hanya berisi bagian `SET`.
Predikat `scope_where` yang tidak kosong ditolak karena predikat bebas harus melewati allowlist lebih dulu.

```sql
-- per batch, satu transaksi target:
UPDATE t SET <up> WHERE key IN (
  SELECT key FROM t WHERE ($1 IS NULL OR key > $1) AND key <= $scope_max ORDER BY key LIMIT $batch_size
) RETURNING key;
SELECT tocatdz_internal.write_checkpoint(...);  -- key persis, cursor, scope, bukti HMAC
COMMIT;
```

- **Cakupan tetap.** `scope_max` diambil sekali sebelum batch pertama dan disimpan di setiap checkpoint. Row yang ditambahkan setelahnya tidak ikut terproses.
- **Tanpa memuat dataset.** Yang ada di memori hanya key dari satu batch.
- **Checkpoint di transaksi yang sama.** Checkpoint ada jika dan hanya jika batch-nya ter-commit. Tabel checkpoint dimiliki role NOLOGIN dan role runtime hanya bisa menulis lewat fungsi yang
  memverifikasi HMAC, sehingga SQL migration tidak bisa memalsukan atau menghapusnya.
- **Batch gagal.** Hanya batch itu yang di-rollback. Batch sebelumnya tetap committed, status menjadi `partial`, dan database ditahan sampai ada permintaan `resume` atau `revert_batches` yang disetujui.
- **Resume** memakai identitas receipt yang sama lalu lanjut dari cursor checkpoint terakhir tanpa mengulang batch.
- **Revert dan rollback** menjalankan `down` pada key yang tersimpan (bukan predikat hari ini) dengan urutan batch terbalik. Setiap batch yang dibatalkan juga punya checkpoint,
  sehingga revert yang terhenti bisa dilanjutkan (`partially_reverted`).
- **Progres di SPA** berasal dari `jobs.batches_committed`, `rows_committed`, dan tabel `job_batches`, yang mencerminkan checkpoint target.

Target yang disiapkan sebelum fitur batch perlu ditambah dukungannya sekali dengan `postgres.UpgradeManagedTargetBatch` (memakai DSN admin target). Target baru mendapatkannya otomatis dari `target-init`.

**Dataset besar.** `cd src && TARGET_ADMIN_DATABASE_URL=postgres://… go run ./cmd/seed-large`, lalu `psql "$TARGET_ADMIN_DATABASE_URL" -f scripts/bench-update.sql`. `seed-large` menggandakan row
`public.orders` sampai UPDATE seluruh row tanpa batching melewati 1 detik; data tidak berubah karena benchmark di-rollback.

| Pengukuran (PostgreSQL 17 di Docker, laptop 16 thread) | Row | Durasi |
|---|---:|---:|
| `seed-large`: UPDATE `orders` tanpa batching | 250.000 | 760 ms |
| `seed-large`: UPDATE `orders` tanpa batching | **500.000** | **1.662 ms** (> 1 detik) |
| Dataset yang sama di test (`items`), UPDATE tanpa batching | 500.000 | 3.081 ms |
| Job batch pada dataset yang sama, `batch_size` 10.000 | 500.000 (50 batch) | 12.887 ms |

Total waktu job batch lebih lama karena setiap batch melakukan commit dan menulis checkpoint. Sebagai gantinya setiap transaksi hanya mengunci 10.000 row, progres terlihat per batch, dan kegagalan hanya
membatalkan satu batch. Ulangi dengan `TEST_DATABASE_URL=… TEST_LARGE_BATCH_ROWS=500000 go test ./internal/postgres -run TestM2BatchLargeDatasetBenchmark -v`.

## Observability

Metrik Prometheus, dashboard Grafana, dan OpenTelemetry Collector dijalankan dengan:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318 docker compose --profile observability up -d --build
```

| Layanan | Alamat | Fungsi |
|---|---|---|
| Grafana | http://localhost:3000 | Dashboard "Tocatdz: Queue, Migration, dan Koneksi" (anonim hanya melihat, admin: `admin`/`admin`) |
| Prometheus | http://localhost:9090 | Mengambil `/metrics` dari server dan worker setiap 5 detik |
| OTel Collector | `:4317` (gRPC), `:4318` (HTTP) | Menerima OTLP, log diteruskan ke Loki dan trace ke Tempo |
| Loki, Tempo | internal | Penyimpanan log dan trace |

Tanpa Docker, jalankan server atau worker dengan `METRICS_ADDR=:9100` lalu buka `http://localhost:9100/metrics`. Listener metrik sengaja terpisah dari API publik; kosongkan `METRICS_ADDR` untuk mematikannya.

### Metrik

Label terbatas pada `status`, `kind`, `mode`, `direction`, dan `pool`. ID user, ID job, dan SQL tidak pernah menjadi label.

| Metrik | Tipe | Arti |
|---|---|---|
| `tocatdz_jobs{status}` | gauge | Jumlah job per status |
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

Waktu tunggu koneksi rata-rata adalah `rate(..._wait_seconds_total) / rate(..._acquires_total)`. Metrik worker hanya terisi pada proses yang menjalankan worker, sedangkan gauge `tocatdz_jobs`
dan metrik pool diekspos oleh server maupun worker, sehingga dashboard memakai `max by (status)` agar tidak menghitung ganda. Dashboard (`deploy/grafana/dashboards/tocatdz.json`, 20 panel) dimuat otomatis
lewat `deploy/grafana/provisioning/`.

Contoh hasil pada delapan job (lima normal, satu batch 300.003 row dengan `batch_size` 10.000, tiga gagal) di dua database yang diproses bersamaan: `max by (status) (tocatdz_jobs)` = succeeded 5 dan failed 3;
`sum by (direction) (tocatdz_rows_processed_total)` = up 300.003; `sum(tocatdz_queue_wait_seconds_count)` = 8; rata-rata durasi mode batch 4,15 detik; pool `platform` aktif 1 dan idle 3.

### Skema log dan trace

Server dan worker menulis satu objek JSON UTF-8 per baris ke stdout (Go `slog`), dan span OpenTelemetry (propagasi W3C TraceContext) selalu dicetak ke stdout. Exporter OTLP/HTTP aktif bila
`OTEL_EXPORTER_OTLP_ENDPOINT` diisi. Tidak ada tabel log di database platform.

| Field | Arti |
|---|---|
| `time`, `level`, `msg`, `event` | Waktu RFC 3339 UTC, level, pesan aman, dan nama event dari katalog |
| `actor` | UUID user, `worker:<id>`, `anonymous`, atau `system` |
| `database_id`, `migration_id`, `change_request_id`, `job_id`, `execution_id` | UUID terkait atau `null` |
| `migration_revision`, `direction`, `attempt`, `batch` | Revisi yang direview, `up`/`down`, attempt dan batch berbasis satu, atau `null` |
| `status` | `start`, `succeeded`, `failed`, `queued`, `running`, `partial`, `uncertain`, `committed`, `rolled_back`, … |
| `request_id`, `trace_id`, `span_id` | ID korelasi buatan server dan konteks trace, atau `null` bila tidak ada |
| `query`, `params`, `duration_ms`, `rows`, `error`, `sqlstate` | SQL driver dengan literal disamarkan, `params` selalu `[REDACTED]`, serta hasil eksekusi |

Event HTTP menambah `http_method`, `http_route` (template, bukan URL mentah), `http_status`, dan `duration_ms`. Tidak pernah dicatat: cookie, header Authorization atau CSRF, kata sandi, DSN, ID sesi, badan
request dan respons, serta query string. SQL disamarkan dengan redactor yang memahami SQL (termasuk dollar quote dan komentar); bila gagal, yang tercatat adalah `[REDACTED SQL]`, dan keluaran query dibatasi 16 KiB
(`query_truncated=true`). Event utama: `http.request`, `auth.login`, `auth.logout`, `migration.created|submitted|approved|rejected`, `job.enqueued|claimed|revalidated|retry_scheduled|succeeded|failed|partial|uncertain|reconciled`,
`lock.acquire.start|acquired|released`, `sql.exec.start|done`, `tx.commit|rollback`, `batch.start|commit|rollback`, dan `history.appended`. `sql.exec.done` yang berhasil belum berarti transaksi ter-commit; commit dicatat terpisah.

Pohon span untuk satu eksekusi:

```text
HTTP POST /api/v1/databases/{database_id}/change-requests/{change_request_id}/execute
  job.enqueue                          (producer)
    job.process                        (consumer; mulai belakangan, trace yang sama)
      lock.acquire
      revalidate
      sql.exec                         (normal)  atau  batch.process (nomor batch sebagai atribut)
        db.query
      history.append
```

Konteks trace dititipkan di `jobs.trace_context` (JSON `traceparent`, maksimum 4096 byte) saat enqueue lalu dipulihkan worker, sehingga span worker tersambung ke request API yang sama.

Contoh satu eksekusi dari API sampai query pada database `demo`: respons `execute` `202` dengan `X-Request-Id: bb53a356adbe1572c5789528d8740da4`, seluruh jejak memakai `trace_id`
`67dca5929411dced7d85f03b74acc820` (114 span di `tocatdz-server` dan `tocatdz-worker`). Di Loki, `{service_name="tocatdz-worker"} | json | job_id="<id>"` menampilkan `job.claimed`, `lock.acquire.start`,
`lock.acquired`, `job.revalidated`, `sql.exec.start`, `sql.exec.done`, `tx.commit`, `history.appended`, `lock.released`, `job.succeeded` dengan `trace_id` yang sama.

## Stress test

Untuk stress test k6, seed akun dengan password yang sama dengan `K6_PASSWORD` (isi `SEED_SHARED_PASSWORD`),
lalu jalankan k6 dengan `LEADER_USERS=admin,pengamat` agar kedua leader menyetujui perubahan pada `demo` dan `gudang`.

## Frontend SPA

Seluruh antarmuka, termasuk login dan admin, adalah SPA React 19 + Vite + TypeScript di `web/` yang
berkomunikasi dengan backend lewat `/api/v1`. Hasil build ditanam ke binary server (`npm run build:embed`),
sehingga satu proses melayani API dan antarmuka. Server menambahkan nonce acak pada `style-src` halaman utama
supaya editor SQL (CodeMirror 6, dimuat lazy) bisa memasang gayanya tanpa melonggarkan `script-src`.

Halaman: login, daftar database, schema, tabel dan data, schema diff, migration (editor SQL, mode normal dan batch,
EXPLAIN), review dan approval, monitor job (polling, progres batch, percobaan, resume dan revert batch),
riwayat eksekusi (salinan SQL, SQL yang dijalankan per batch, hide rollback, rollback dan forward), serta admin
(user, group, koneksi, akses). Rujukan kontrak ada di `api/openapi.yaml`, tipenya dibangkitkan dengan `npm run gen:api`.

Perintah dari folder `web/`:

| Perintah | Fungsi |
| --- | --- |
| `npm run dev:mock` | Server dev dengan API palsu (MSW); buka `/?as=<username>` untuk melewati login |
| `npm test` | Unit dan komponen (vitest), berjalan di atas handler mock yang sama |
| `npm run lint` dan `npx tsc -b` | ESLint dan pemeriksaan tipe |
| `npx playwright test` | Smoke test browser terhadap `dev:mock` (desktop 1280 px dan mobile 390 px) |

### Pengujian terhadap server sungguhan

Spec `web/e2e/real*.spec.ts` berjalan terhadap server Go dan worker yang asli (PostgreSQL, antrean, worker, CSP). Seed harus
sudah dijalankan; akun `editor` mengajukan, `admin` dan `pengamat` menjadi leader yang menyetujui.

```sh
cd web
E2E_BASE_URL=http://localhost:8080 E2E_ADMIN_PASSWORD=... E2E_VIEWER_PASSWORD=... E2E_EDITOR_PASSWORD=... npx playwright test --project=desktop   # lalu --project=mobile
```

Setiap peran masuk satu kali (`real-setup.ts`) karena login dibatasi 5 per akun per menit. Cakupannya: alur lengkap buat, ajukan,
approve oleh semua leader, jalankan, pantau job, riwayat, rollback, dan forward; batch yang berhenti di batch kedua lalu
dipulihkan dengan revert batch; migration irreversible; satu permintaan terbuka per database; akses lihat saja; serta
pengukuran scroll horizontal semua halaman pada lebar 360 dan 1440 px dengan data nyata.

### Hasil stress test k6

Skrip `loadtest/k6/stress.js` menjalankan dua skenario sekaligus: pembacaan (schema, posisi, riwayat, daftar, baris tabel) dan alur job
lengkap (buat, ajukan, disetujui dua leader, dijalankan worker, dipantau sampai status akhir) pada database `demo` dan `gudang`.
Beban naik 1×, 2×, 4×, 8×, lalu turun ke 1× (sampai 80 VU baca dan 16 VU job, ±8 menit). Keberhasilan job dihitung dari status akhir job,
bukan dari respons 202. Variabel yang bisa diatur: `PROFILE=smoke`, `SCENARIOS=reads|jobs`, `READ_WORKFLOW=false`,
`K6_PASSWORD_<USERNAME>`, `SESSIONS_PER_ACCOUNT`, `MULTIPLIERS`, `RAMP_SECONDS`, `HOLD_SECONDS`, `DB_COUNT`, `SAME_DB`.

```sh
docker run --rm --network <jaringan-compose> -v "$PWD/loadtest/k6:/scripts:ro"   -e BASE_URL=http://server:8080 -e ORIGIN=http://localhost:8080 -e LEADER_USERS=admin,pengamat   -e K6_PASSWORD_EDITOR=... -e K6_PASSWORD_ADMIN=... -e K6_PASSWORD_PENGAMAT=...   grafana/k6 run /scripts/stress.js
```

Pengukuran dilakukan di satu laptop Windows dengan Docker Desktop (server, worker dengan `WORKER_CONCURRENCY=2`, PostgreSQL, dan k6 semuanya di mesin yang sama),
sehingga angka mutlaknya hanya berlaku untuk lingkungan itu. Keluaran lengkap dan ringkasan JSON ada di `docs/k6/`.

| Metrik (seluruh run) | Server apa adanya | Dengan perbaikan sesi |
| --- | --- | --- |
| Request gagal | 25,3 % | 1,9 % |
| Job sukses (status akhir) | 39,6 % (40 dari 101) | 93,3 % (97 dari 104) |
| Throughput | 11,5 request/s | 13,5 request/s |
| Waktu antre job p50 / p95 | 538 / 1065 ms | 905 / 1677 ms |
| Durasi job p50 / p95 | 904 / 1157 ms | 1260 / 2072 ms |

Rincian per tahap pada server dengan perbaikan sesi:

| Tahap | Request gagal | Baca p50 | Baca p95 | Throughput | Job selesai | Antre job p95 |
| --- | --- | --- | --- | --- | --- | --- |
| 1× | 0 % | 378 ms | 662 ms | 2,4/s | 30 | 1173 ms |
| 2× | 0 % | 1091 ms | 1627 ms | 2,7/s | 20 | 1405 ms |
| 4× | 0 % | 2586 ms | 3667 ms | 2,8/s | 14 | 1393 ms |
| 8× | 8,5 % | 5877 ms | 6772 ms | 2,8/s | 7 | 1730 ms |
| 1× (turun) | 0,7 % | 624 ms | 5328 ms | 2,6/s | 26 | 1723 ms |

Analisis:
- **Throughput mendatar di ±2,8 request/s** sementara latensi baca naik hampir linear dengan beban, tanda antrean pada satu mesin. Pada 8× server menolak sebagian
  `GET /databases` dengan 503 (batas waktu) dan kembali sehat saat beban turun. Waktu antre job hampir tidak berubah (1,2–1,7 detik), jadi worker bukan penyempit.
- **Cacat yang ditemukan oleh stress test ini:** `AuthService.Authenticate` menulis baris sesi dengan compare-and-swap pada *setiap* request dan hanya mencoba 3 kali. Request paralel pada satu
  sesi saling mengalahkan lalu dibalas 409 (5 request paralel: 2 gagal, 40: 35 gagal; reproduksi dengan `loadtest/k6/diagnostics/session-contention.mjs`). Kolom "Server apa adanya" pada tabel di atas
  adalah akibatnya. Perbaikannya (menulis sesi hanya saat rotasi, kenaikan hak, atau `last_seen` lebih tua dari 30 detik) kini diterapkan di `AuthService.Authenticate` dan menjadikan 40 request paralel
  semuanya 200; `docs/k6/usulan-perbaikan-sesi.patch` disimpan sebagai bukti asalnya. Angka pada tabel di atas diukur sebelum perbaikan ini masuk ke kode: kolom "Server apa adanya" tanpa perbaikan,
  kolom "Dengan perbaikan sesi" dengan patch yang sama. `last_seen` kini bisa tertinggal hingga 30 detik dari aktivitas terakhir, jauh di bawah batas idle 30 menit.
- Respons 409 pada `POST .../change-requests` adalah penolakan yang diharapkan (satu permintaan terbuka per database); skrip mengulang pengajuan dan mencatatnya sebagai `submit_conflicts`.
- Ambang `p(95)<500 ms` untuk pembacaan tidak terpenuhi bahkan pada 1× (662 ms) di mesin ini, sedangkan `http_req_failed<1 %` dan `job_success>95 %` tidak terpenuhi karena sisa kegagalan pada beban 8×.
- Login dibatasi 5 per akun per menit dan tidak bisa diatur, sehingga k6 memakai kolam sesi kecil per akun (maksimal 5) dan `setup()` menunggu `Retry-After` bila batas itu terpakai.

## Akun demo (dari `cmd/seed`)

| Username | Password | Peran |
| --- | --- | --- |
| `admin` (atau `SEED_ADMIN_USERNAME`) | `SEED_ADMIN_PASSWORD` | Platform admin, leader group `pengembang` (View + Edit ke `demo` dan `gudang`) |
| `pengamat` | `SEED_VIEWER_PASSWORD` | Leader group `pengamat` (View Only ke `demo` dan `gudang`) |
| `editor` | `SEED_EDITOR_PASSWORD` | Anggota group `pengembang`, mengajukan perubahan migration |

Admin menjadi leader group `pengembang`. Perubahan pada `demo` atau `gudang` perlu
disetujui leader kedua group, sedangkan pemohon tidak boleh menyetujui CR miliknya.

Password tidak disimpan di repo; masing-masing anggota tim mengisinya di `.env` lokal.

## Test

```
go test ./...                 # unit + integration in-memory
go vet ./...
go test -race ./...           # butuh toolchain dengan cgo
```

Integration test PostgreSQL berjalan bila `TEST_DATABASE_URL` diset ke server khusus test, contoh:
```
TEST_DATABASE_URL=postgres://app:app@127.0.0.1:5433/postgres?sslmode=disable go test ./...
```
Setiap test membuat database `tocatdz_test_*`/`tocatdz_e2e_*` sendiri lalu menghapusnya. Jangan arahkan ke database yang berisi data penting. Tanpa `TEST_DATABASE_URL`, test tersebut otomatis dilewati.

Integration test MySQL berjalan bila `TEST_MYSQL_TARGET_DSN` diset. Akun pada DSN pengujian harus dapat membuat dan menghapus database sementara, contoh:

```text
TEST_MYSQL_TARGET_DSN=root:password@tcp(127.0.0.1:3306)/mysql go test ./...
```

Tanpa `TEST_MYSQL_TARGET_DSN`, test MySQL otomatis dilewati. Workflow `.github/workflows/database-integration.yml` menjalankan integration test PostgreSQL dan MySQL pada setiap pull request yang mengubah source code.

## Keputusan desain

| Topik | Keputusan |
| --- | --- |
| Password | Argon2id (m=19456 KiB, t=2, p=1), minimal 15 karakter, maksimal 1024 byte |
| Session | ID acak 32 byte di cookie `tocatdz_session` (`HttpOnly`, `SameSite=Lax`, `Secure` di HTTPS); data session di PostgreSQL |
| Batas waktu | Idle 30 menit, absolute 12 jam, rotasi tiap 15 menit, grace token lama 30 detik |
| Rotasi | Saat login, saat hak akses naik, dan berkala; rotasi tidak memperpanjang absolute expiration |
| Logout / pencabutan | Session dicabut di database; pengurangan hak (hapus anggota, turunkan/cabut grant, nonaktifkan user) mencabut session terdampak |
| CSRF | Cek Origin/Referer persis + token HMAC terikat session di setiap form POST |
| Akses database | Hanya lewat group. Jika user ada di beberapa group untuk database yang sama, akses efektif = yang tertinggi (`view_edit` > `view_only`) |
| Platform admin | Mengelola user/group/koneksi/akses, tetapi tetap butuh grant group untuk melihat atau mengubah isi database |
| Koneksi database target | Connection string dienkripsi AES-256-GCM (`CONNECTION_ENCRYPTION_KEY`) dan tidak pernah ditampilkan ulang |
| Migration PostgreSQL | Validasi SQL dengan parser PostgreSQL; eksekusi dalam satu transaction (commit/rollback); riwayat linear per database dengan versi monoton; riwayat yang sudah dieksekusi tidak bisa diubah |
| Migration MySQL | Validasi menggunakan aturan SQL MySQL; DML memakai transaction, sedangkan DDL yang mengalami implicit commit ditangani dengan execution journal dan dipertahankan sebagai `pending` bila hasil akhirnya tidak dapat dipastikan |

Kontrak keamanan dan catatan audit dibagikan terpisah untuk koordinasi kelompok; signature API pada kode Go menjadi acuan implementasi.

## Security: CodeQL

Workflow CodeQL menganalisis Go dan JavaScript/TypeScript memakai query suite
`security-extended` pada PR/push ke `main`, jadwal mingguan, dan trigger manual.
Hasil dikirim ke GitHub Code scanning (tab Security) serta disimpan sebagai artifact SARIF dan
metadata per run, sehingga temuan, status triase, dan bukti scan ulang setelah perbaikan dapat dilihat di sana.

**Cakupan.** Go: seluruh paket yang dikompilasi `go build ./...` (berkas `_test.go` tidak ikut karena tidak dibangun), 35 aturan dijalankan. JavaScript/TypeScript: seluruh berkas terlacak di `web/`, `loadtest/`, dan sekitarnya, kecuali `node_modules`, `dist`, `vendor`, `coverage`, `playwright-report`, dan `test-results`, 103 aturan dijalankan. Query suite `security-extended`. Daftar berkas yang dipindai tiap run ada di artifact `source-inventory.txt`.

**Temuan dan keputusan.** Scan pada commit `0eee45c` menghasilkan dua alert, keduanya severity *high* dan keduanya diperbaiki di commit `20fc097`:

| # | Aturan | Lokasi | Dampak | Keputusan |
| --- | --- | --- | --- | --- |
| 1 | `js/clear-text-logging` | `loadtest/k6/diagnostics/session-contention.mjs:12` | Skrip diagnostik mencetak nilai dari variabel environment (`USERNAME`) saat login gagal. Yang tercetak nama akun, bukan kata sandi, dan skrip hanya dipakai manual. | **Diperbaiki.** Pesan galat tidak lagi memuat nama akun. |
| 2 | `go/incorrect-integer-conversion` | `src/internal/security/password_hasher.go:87` | Konversi `uint8(values[2])` dari hasil `strconv.ParseUint`. Dalam praktik nilainya sudah dibatasi 1 sampai 4 sebelum konversi, sehingga tidak ada dampak nyata; CodeQL tidak melihat batas itu karena nilainya disimpan dalam array. | **Diperbaiki.** Nilai dipindahkan ke variabel lokal dan batasnya diperiksa langsung sebelum konversi, agar batas itu eksplisit dan terbaca analisis. |

Tidak ada alert yang diterima sebagai risiko atau ditandai *false positive*. Hasil bersih tidak berarti aplikasi pasti aman: CodeQL hanya menganalisis kode statis dan tidak mencakup konfigurasi, dependensi, maupun perilaku saat berjalan.

**Scan ulang.** Analisis CodeQL pada PR #54 (head `978101c`) berisi kedua perbaikan dan menghasilkan 0 temuan untuk Go (35 aturan) dan untuk JavaScript/TypeScript (103 aturan); tidak ada alert terbuka pada ref PR tersebut.

## UI Credits

- Tata letak dan gaya: [Bootstrap Simple Admin Template](https://github.com/alexisbriandev/bootstrap-simple-admin-template) (Apache License 2.0).
- Bootstrap 5.3.3 (MIT), Font Awesome Free 6 solid (CC BY 4.0 / SIL OFL 1.1 / MIT), dan font Lato (SIL OFL 1.1), semuanya diambil dari folder aset template tersebut. Lisensi disertakan di `web/public/licenses` dan ikut tersaji di `/licenses/*.txt`.

## Struktur

```
src/
├── cmd/server       # aplikasi web
├── cmd/seed         # data demo
├── cmd/keygen       # pembuat kunci .env
├── cmd/target-init  # setup akun terbatas untuk database target tambahan
└── internal/
    ├── security    # password, session, CSRF, otorisasi
    ├── explorer    # domain explorer database
    ├── migration   # domain migration (validator, executor, rekonsiliasi, EXPLAIN)
    ├── schemadiff  # bonus schema diff dan pembuat kandidat SQL
    ├── admin       # domain administrasi platform
    ├── postgres    # repository PostgreSQL + adapter target PostgreSQL
    ├── mysql       # adapter database viewer dan migration target MySQL
    ├── targetdb    # router dan validator berdasarkan DBMS target
    ├── bootstrap   # perakitan semua komponen
    ├── spa         # menyajikan hasil build SPA yang ditanam ke binary (CSP nonce, fallback rute klien)
    └── web         # router dan middleware fondasi

web/                # frontend SPA (React, Vite, TypeScript)
loadtest/k6/        # skrip stress test k6 dan tesnya
```
