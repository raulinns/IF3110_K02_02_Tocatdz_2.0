# IF3110_K02_02_Tocatdz_2.0

Tugas Besar Milestone 1 IF3110 Pengembangan Aplikasi Web: Database Management Platform berbasis Go. PostgreSQL digunakan sebagai database metadata platform, sedangkan database target dapat menggunakan PostgreSQL atau MySQL.

Alur yang didukung: login → akses lewat group → pilih database → lihat schema/table/data → buat migration → jalankan (commit/rollback) → riwayat migration. Platform admin mengelola user, group, koneksi database, dan akses.

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
   docker compose up -d
   docker compose ps
   ```

   Konfigurasi Docker bawaan membuat PostgreSQL di `localhost:5433`, database
   `platform` untuk metadata aplikasi, database `demo` sebagai target contoh,
   dan user/password lokal `app`/`app`. Tunggu sampai service berstatus
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
   acak. Isi juga `SEED_ADMIN_PASSWORD` dan, jika akun pengamat diperlukan,
   `SEED_VIEWER_PASSWORD`; setiap password minimal 15 karakter. Jangan menambahkan
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

4. **Jalankan server.**

   ```sh
   go run ./cmd/server
   ```

   Buka `http://localhost:8080`. Alamat yang dibuka di browser harus sama persis
   dengan nilai `PUBLIC_ORIGIN`.

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

## UI Credits

- Tata letak dan gaya: [Bootstrap Simple Admin Template](https://github.com/alexisbriandev/bootstrap-simple-admin-template) (Apache License 2.0).
- Bootstrap 5.3.3 (MIT), Font Awesome Free 6 solid (CC BY 4.0 / SIL OFL 1.1 / MIT), dan font Lato (SIL OFL 1.1), semuanya diambil dari folder aset template tersebut. Lisensi disertakan di `src/internal/ui/static`.

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
    ├── ui          # handler HTML, template, CSS
    └── web         # router dan middleware fondasi
```
