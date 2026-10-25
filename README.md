# IF3110_K02_02_Tocatdz_2.0

Tugas Besar Milestone 1 IF3110 Pengembangan Aplikasi Web: Database Management Platform berbasis Go + PostgreSQL.

Alur yang didukung: login → akses lewat group → pilih database → lihat schema/table/data → buat migration → jalankan (commit/rollback) → riwayat migration. Platform admin mengelola user, group, koneksi database, dan akses.

## Prasyarat

- Go 1.27.3 atau lebih baru
- PostgreSQL 17/18 (lewat Docker atau terpasang lokal)

## Menjalankan secara lokal

Semua perintah `go` dijalankan dari folder `src/`.

1. **Siapkan PostgreSQL.**
   - Docker (paling mudah): dari root repo `docker compose up -d`. Ini membuat server di `localhost:5433` dengan database `platform` (metadata aplikasi) dan `demo` (database target contoh), user/password `app`/`app`.
   - PostgreSQL lokal: buat dua database sendiri, lalu sesuaikan `DATABASE_URL` dan `SEED_TARGET_DATABASE_URL`.
2. **Buat `.env`** di root repo dari `.env.example`, lalu isi tiga kunci dengan output:
   ```
   go run ./cmd/keygen
   ```
   Isi juga `SEED_ADMIN_PASSWORD` dan (opsional) `SEED_VIEWER_PASSWORD`, masing-masing minimal 15 karakter. File `.env` sudah di-ignore git; jangan pernah commit kunci atau password.
3. **Isi data demo** (membuat skema platform, akun, group, akses, dan tabel contoh di database target; aman dijalankan berulang):
   ```
   go run ./cmd/seed
   ```
   Seed memakai kredensial pemilik target untuk setup, lalu menyimpan DSN akun SQL terbatas yang dibuat otomatis. Akun superuser tidak dipakai untuk menjalankan SQL migration. Untuk instalasi lokal, akun setup perlu superuser dan extension `pgcrypto` harus tersedia. Gunakan database target khusus aplikasi; setup memindahkan kepemilikan objek aplikasi milik akun setup ke akun SQL tersebut.
4. **Jalankan server:**
   ```
   go run ./cmd/server
   ```
   Buka `http://localhost:8080` (harus sama persis dengan `PUBLIC_ORIGIN`).

Skema database platform diterapkan otomatis saat server atau seed berjalan (`src/internal/postgres/schema/*.sql`).

## Akun demo (dari `cmd/seed`)

| Username | Password | Peran |
| --- | --- | --- |
| `admin` (atau `SEED_ADMIN_USERNAME`) | `SEED_ADMIN_PASSWORD` | Platform admin, anggota group `pengembang` (View + Edit ke `demo`) |
| `pengamat` | `SEED_VIEWER_PASSWORD` | Anggota group `pengamat` (View Only ke `demo`) |

Password tidak disimpan di repo; masing-masing anggota tim mengisinya di `.env` lokal.

## Test

```
go test ./...                 # unit + integration in-memory
go vet ./...
go test -race ./...           # butuh toolchain dengan cgo
```

Integration test PostgreSQL berjalan bila `TEST_DATABASE_URL` diset ke server khusus test, contoh:
```
TEST_DATABASE_URL=postgres://app:app@localhost:5433/postgres?sslmode=disable go test ./...
```
Setiap test membuat database `tocatdz_test_*`/`tocatdz_e2e_*` sendiri lalu menghapusnya. Jangan arahkan ke database yang berisi data penting. Tanpa `TEST_DATABASE_URL`, test tersebut otomatis dilewati.

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
| Migration | Validasi SQL dengan parser PostgreSQL; eksekusi dalam satu transaction (commit/rollback); riwayat linear per database dengan versi monoton; riwayat yang sudah dieksekusi tidak bisa diubah |

Kontrak keamanan dan catatan audit dibagikan terpisah untuk koordinasi kelompok; signature API pada kode Go menjadi acuan implementasi.

## Integrasi database-foundation

`cmd/server` dan `cmd/seed` memakai schema aplikasi di `internal/postgres/schema`.
Startup server merakit UI, auth/session, admin, explorer, migration, dan worker
recovery melalui `bootstrap.Build`; pool PostgreSQL memakai fondasi `platformdb.Open`.
Jangan mengganti startup ini dengan `platformdb.OpenMigrated`, karena fungsi itu
masih menerapkan schema foundation awal untuk modul SQLC.

Schema foundation (`user_groups`, `group_database_access`, `migration_history`)
dan schema aplikasi (`group_memberships`, `group_database_grants`, `migrations`)
memiliki kontrak yang berbeda. Kedua migrator mendeteksi schema yang tidak sesuai
dan menolak perubahan. Data foundation yang sudah ada memerlukan migrasi data
eksplisit; tidak ada penghapusan atau konversi otomatis. Gunakan database platform
baru untuk menjalankan aplikasi jika sebelumnya hanya memakai schema foundation.

Registry/driver dan reader MySQL tersedia pada paket foundation, tetapi UI admin
serta executor aplikasi saat ini tetap PostgreSQL. Ini belum merupakan bonus
MySQL lengkap. Schema diff/generation belum tersedia. Bonus query execution plan
tersedia pada detail draft migration: EXPLAIN untuk SELECT/INSERT/UPDATE/DELETE,
dan EXPLAIN ANALYZE untuk SELECT baca saja.

Receipt migration dimiliki role `NOLOGIN` yang terpisah dari akun SQL target.
Akun SQL tidak bisa membaca kunci, mengubah tabel receipt, atau menjadi pemiliknya.
Penulisan receipt melalui fungsi terbatas membutuhkan HMAC yang terikat target
dan identitas eksekusi; receipt dan SQL di-commit dalam transaksi yang sama.
Recovery memakai advisory lock transaksi untuk memastikan transaksi lama benar-benar
selesai, lalu membaca receipt tanpa menjalankan ulang SQL. Worker memeriksa status
`pending` saat startup dan setiap 30 detik.

Setelah memperbarui dari versi yang menyimpan DSN superuser, hentikan server,
jalankan seed lagi dengan nama dan database target yang sama, lalu hidupkan server.
Seed mempertahankan akun, password, ID database, dan riwayat; target fisik yang
berbeda dengan nama lama ditolak. Jangan mengganti kunci enkripsi pada `.env`.

Untuk menambah database target lain melalui halaman admin, ikuti setup
`cmd/target-init` dan checklist pada [panduan testing wajib](docs/testing-wajib.md).
Database metadata platform selalu ditolak sebagai target.

Test PostgreSQL membuat database sementara per kasus, termasuk test foundation,
sehingga schema kedua jalur tidak bercampur. Aktifkan `TEST_DATABASE_URL` untuk
test repository dan end-to-end aplikasi; `TEST_POSTGRES_TARGET_DSN` mengaktifkan
test reader foundation, dan `TEST_MYSQL_TARGET_DSN` hanya untuk target MySQL test.

Contoh PowerShell setelah server PostgreSQL test tersedia:

```powershell
$env:TEST_DATABASE_URL = 'postgres://app:app@localhost:5433/postgres?sslmode=disable'
$env:TEST_POSTGRES_TARGET_DSN = $env:TEST_DATABASE_URL
go test -race -tags=argon2integration -count=1 ./...
```

## Bonus query execution plan

Panduan bonus: [testing query execution plan](docs/testing-query-plan.md).
Analisis memerlukan View + Edit, memakai revision SQL draft tersimpan dan
transaksi read-only, tidak memberi versi migration atau menulis receipt.
Untuk script beberapa statement, pilih nomor statement; DDL sebelumnya tidak
dijalankan. ANALYZE query mutasi ditolak agar preview tidak memicu sequence,
trigger, atau efek lain yang belum tentu dapat dipulihkan oleh rollback.
Tidak ada perubahan schema metadata atau kunci `.env`; restart server Go setelah
memperbarui kode untuk memakai fitur ini.

## Struktur

```
src/
├── cmd/server   # aplikasi web
├── cmd/seed     # data demo
├── cmd/keygen   # pembuat kunci .env
└── internal/
    ├── security   # password, session, CSRF, otorisasi
    ├── explorer   # domain explorer database
    ├── migration  # domain migration (validator, executor, rekonsiliasi)
    ├── admin      # domain administrasi platform
    ├── postgres   # repository PostgreSQL + koneksi database target
    ├── bootstrap  # perakitan semua komponen
    ├── ui         # handler HTML, template, CSS
    └── web        # router dan middleware fondasi
```
