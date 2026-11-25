# Worker Migration: Queue, Lock, Retry, dan Pemulihan

Dokumen ini menjelaskan cara `cmd/worker` menjalankan job migration (lane 13524044, Q-01 sampai Q-07).
Ringkasnya, setiap job dijalankan **paling banyak satu kali secara efektif**: queue boleh mengirim ulang job
(*at-least-once*), tetapi receipt di database target menjamin commit tidak terjadi dua kali.

## Alur satu job

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

## Konkurensi

| Aturan spesifikasi | Mekanisme |
|---|---|
| Satu database menjalankan satu migration | Claim melewati database yang punya job `running`/`uncertain`. Advisory lock di target menjadi pengaman lintas proses dan alias koneksi. |
| Database berbeda diproses paralel | `WORKER_CONCURRENCY` loop per proses, dan beberapa proses `cmd/worker` boleh berjalan bersamaan. `SKIP LOCKED` membuat worker tidak saling menunggu. |
| Lock berlaku lintas worker dan di antara commit batch | Lock bersifat session-level (`pg_advisory_lock`) pada satu koneksi khusus. Lock ini tidak lepas saat transaksi batch di-commit. |
| Pesan duplikat tidak menyebabkan double commit | `UNIQUE(change_request_id)` pada `jobs`, lease token yang memagari worker lama, dan receipt per `execution_id` yang diperiksa sebelum SQL dijalankan. |

## Retry

- Retry hanya untuk kegagalan sementara: SQLSTATE `40001`, `40P01`, `55P03`, kelas `08`, serta `57P01`–`57P03`.
  Retry juga berlaku jika target tidak bisa dihubungi sebelum ada SQL yang dikirim.
- Jumlah attempt dibatasi `max_attempts`. Jeda memakai backoff eksponensial (2 detik, 4 detik, … maksimum 1 menit)
  dengan jitter.
- Setiap attempt, termasuk yang gagal, tercatat di `job_attempts` (spesifikasi revisi 4). Attempt yang
  sudah selesai tidak bisa diubah.
- Error SQL permanen (misalnya `42P01`) langsung menjadi `failed`. Transaksi di-rollback, history tidak bertambah,
  dan posisi aktif tetap.

## Pemulihan lock ketika worker gagal

1. **Worker mati saat memegang lock.** Koneksi ke target terputus, dan PostgreSQL otomatis melepas
   advisory lock session-level milik koneksi itu. Lock tidak pernah tertinggal.
2. **Transaksi yang belum di-commit** ikut di-rollback oleh PostgreSQL ketika koneksi putus.
3. **Lease job kedaluwarsa** karena heartbeat berhenti. Loop rekonsiliasi (`RecoverOnce`) mengklaim ulang
   job dengan menaikkan `lease_token`, sehingga worker lama tidak bisa lagi mencatat hasil.
4. Rekonsiliasi **tidak menjalankan SQL**. Rekonsiliasi hanya membaca bukti di target di bawah lock:
   - belum ada `execution_plan` → SQL belum pernah dikirim → job dijadwalkan ulang;
   - receipt cocok → commit terbukti → `CompleteCommitted`;
   - tidak ada receipt dan target tenang → tidak ada commit → retry atau `failed`;
   - bukti tidak bisa dipastikan → `uncertain`. Database ditahan (barrier) sampai hasilnya terbukti.
5. Jika commit target berhasil tetapi pencatatan di platform gagal, job tetap `running` sampai lease habis,
   lalu langkah 4 menyelesaikannya dari receipt. Commit kedua tidak mungkin terjadi.

## Menjalankan

```bash
cd src && go run ./cmd/worker
```

Worker memakai `.env` yang sama dengan server, termasuk `DATABASE_URL` dan `CONNECTION_ENCRYPTION_KEY`
(kunci keamanan lain tetap divalidasi). Variabel khusus worker: `WORKER_ID` (default `hostname-pid`) dan
`WORKER_CONCURRENCY` (default 2, rentang 1–64).

Worker juga bisa berjalan di dalam proses server dengan `EMBEDDED_WORKER=true` untuk pengembangan lokal.
Revalidasi setelah lock memakai `migration.NewRevalidator`; worker menolak start jika revalidator tidak tersedia.

## Mode batch (K4, B-01 sampai B-03)

Konfigurasi batch: `schema`, `table`, `key_column` (integer, unik, NOT NULL, tidak boleh diubah oleh SET),
dan `batch_size` (100–100.000). `up_sql`/`down_sql` hanya berisi bagian `SET`. Versi ini menolak `scope_where`
karena predikat bebas harus melewati allowlist terlebih dahulu sebelum boleh disisipkan ke query.

```sql
-- per batch, satu transaksi target:
UPDATE t SET <up> WHERE key IN (
  SELECT key FROM t WHERE ($1 IS NULL OR key > $1) AND key <= $scope_max ORDER BY key LIMIT $batch_size
) RETURNING key;
SELECT tocatdz_internal.write_checkpoint(...);  -- key persis, cursor, scope, bukti HMAC
COMMIT;
```

- **Cakupan tetap.** `scope_max` diambil sekali sebelum batch pertama dan disimpan di setiap checkpoint. Row yang
  ditambahkan setelahnya tidak ikut terproses.
- **Tanpa memuat dataset.** Yang ada di memori hanya key dari satu batch.
- **Checkpoint di transaksi yang sama.** Checkpoint ada jika dan hanya jika batch-nya ter-commit. Tabel checkpoint
  dimiliki role NOLOGIN, dan role runtime hanya bisa menulis lewat fungsi yang memverifikasi HMAC. Dengan begitu,
  SQL migration tidak bisa memalsukan atau menghapus checkpoint.
- **Batch gagal.** Hanya batch itu yang di-rollback. Batch sebelumnya tetap committed, status menjadi `partial`, dan
  database ditahan (barrier) sampai ada CR `resume` atau `revert_batches` yang di-approve.
- **Resume** memakai identitas receipt yang sama, lalu lanjut dari cursor checkpoint terakhir tanpa mengulang batch.
- **Revert/rollback** menjalankan `down` pada **key yang tersimpan** (bukan predikat hari ini) dengan urutan batch
  terbalik. Setiap batch yang dibatalkan juga punya checkpoint, sehingga revert yang terhenti bisa dilanjutkan
  (`partially_reverted`).
- **Progres SPA.** `jobs.batches_committed`/`rows_committed` dan tabel `job_batches` adalah cermin checkpoint target.

Target yang disiapkan sebelum Milestone 2 perlu ditambah dukungan batch sekali saja dengan
`postgres.UpgradeManagedTargetBatch` (memakai DSN admin target). Target baru otomatis mendapatkannya dari
`target-init`.

### Dataset besar (B-03)

```bash
cd src && TARGET_ADMIN_DATABASE_URL=postgres://… go run ./cmd/seed-large
psql "$TARGET_ADMIN_DATABASE_URL" -f scripts/bench-update.sql
```

`seed-large` menggandakan jumlah row pada `public.orders` sampai UPDATE seluruh row tanpa batching melewati
1 detik. Data tidak berubah karena benchmark di-rollback.

Hasil pada lingkungan pengujian (PostgreSQL 17 di Docker, laptop 16 thread, 22 November 2026):

| Pengukuran | Row | Durasi |
|---|---:|---:|
| `seed-large`: UPDATE `orders` tanpa batching | 250.000 | 760 ms |
| `seed-large`: UPDATE `orders` tanpa batching | **500.000** | **1.662 ms** (> 1 detik) |
| Dataset yang sama di test (`items`), UPDATE tanpa batching | 500.000 | 3.081 ms |
| Job batch pada dataset yang sama, `batch_size` 10.000 | 500.000 (50 batch) | 12.887 ms |

Total waktu job batch lebih lama karena setiap batch melakukan commit dan menulis checkpoint beserta key-nya.
Sebagai gantinya, setiap transaksi hanya mengunci 10.000 row, progres terlihat per batch, dan kegagalan hanya
membatalkan satu batch. Benchmark bisa diulang dengan perintah berikut:

```bash
TEST_DATABASE_URL=… TEST_LARGE_BATCH_ROWS=500000 go test ./internal/postgres -run TestM2BatchLargeDatasetBenchmark -v
```
