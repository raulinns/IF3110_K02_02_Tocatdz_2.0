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

Paket `targetdb` memilih adapter PostgreSQL atau MySQL berdasarkan engine yang
tersimpan bersama koneksi. Kedua DBMS memakai access control, database viewer,
editor migration, eksekusi, dan riwayat yang sama. Bonus query execution plan
tersedia pada detail draft migration PostgreSQL, sedangkan bonus schema diff
tersedia pada tab Schema diff database PostgreSQL. Kedua fitur khusus PostgreSQL
tersebut disembunyikan dan ditolak pada backend untuk koneksi MySQL.

Pada target PostgreSQL, receipt migration dimiliki role `NOLOGIN` yang terpisah
dari akun SQL target. Akun SQL tidak bisa membaca kunci, mengubah tabel receipt,
atau menjadi pemiliknya. Penulisan receipt melalui fungsi terbatas membutuhkan
HMAC yang terikat target dan identitas eksekusi; receipt dan SQL di-commit dalam
transaksi yang sama. Recovery memakai advisory lock transaksi untuk memastikan
transaksi lama benar-benar selesai, lalu membaca receipt tanpa menjalankan ulang
SQL.

Pada target MySQL, akun koneksi target membuat dan memperbarui tabel journal
`_tocatdz_migration_receipts`. Tabel tersebut disembunyikan dari database viewer
dan tidak boleh dirujuk oleh SQL migration yang melewati validator aplikasi.
Model ini tidak memberikan pemisahan role sekuat PostgreSQL, tetapi journal tetap
digunakan untuk mencegah eksekusi ulang ketika DDL melakukan implicit commit.
Worker memeriksa status `pending` saat startup dan setiap 30 detik.

Setelah memperbarui dari versi yang menyimpan DSN superuser, hentikan server,
jalankan seed lagi dengan nama dan database target yang sama, lalu hidupkan server.
Seed mempertahankan akun, password, ID database, dan riwayat; target fisik yang
berbeda dengan nama lama ditolak. Jangan mengganti kunci enkripsi pada `.env`.

Untuk menambah database target lain melalui halaman admin, isi
`TARGET_ADMIN_DATABASE_URL` (akun pemilik/superuser database target) lalu
jalankan `go run ./cmd/target-init`. DSN akun terbatas hasil setup ditulis ke
`target-runtime.env` (diabaikan git); salin `TARGET_DATABASE_URL` dari file itu ke
halaman admin Koneksi. Database metadata platform selalu ditolak sebagai target.

Test PostgreSQL membuat database sementara per kasus, termasuk test foundation,
sehingga schema kedua jalur tidak bercampur. Aktifkan `TEST_DATABASE_URL` untuk
test repository dan end-to-end aplikasi; `TEST_POSTGRES_TARGET_DSN` mengaktifkan
test reader foundation.

Test MySQL juga membuat database sementara per kasus. Aktifkan
`TEST_MYSQL_TARGET_DSN` untuk menguji koneksi, viewer, access control, migration,
execution journal, dan alur end-to-end melalui handler HTML.

Contoh PowerShell setelah server PostgreSQL test tersedia:

```powershell
$env:TEST_DATABASE_URL = 'postgres://app:app@localhost:5433/postgres?sslmode=disable'
$env:TEST_POSTGRES_TARGET_DSN = $env:TEST_DATABASE_URL
go test -race -tags=argon2integration -count=1 ./...
```

## Bonus dukungan DBMS tambahan MySQL

Platform admin dapat menyimpan koneksi MySQL melalui UI, kemudian memberi akses
`View Only` atau `View + Edit` kepada group seperti pada database PostgreSQL.
Database viewer MySQL menampilkan schema aktif, table/view, column, constraint,
index, dan data. Pengguna `View + Edit` dapat membuat, mengubah, dan menjalankan
migration; pengguna `View Only` hanya dapat melihat data dan riwayat migration.

MySQL melakukan implicit commit pada banyak operasi DDL, seperti `CREATE TABLE`
dan `ALTER TABLE`. Karena itu, kegagalan setelah DDL tidak selalu dapat di-rollback
secara penuh. Aplikasi menulis execution journal sebelum menjalankan migration.
Jika hasil eksekusi tidak dapat dibuktikan dengan aman, migration tetap berstatus
`pending` dan tidak dijalankan ulang secara otomatis. Perilaku ini mencegah sistem
menampilkan status rollback yang tidak sesuai dengan kondisi database sebenarnya.

Schema diff, migration generation, dan query execution plan analysis belum
diterapkan untuk MySQL. Route terkait ditolak oleh backend dan tab-nya tidak
ditampilkan pada database MySQL.

## Bonus query execution plan

Buka detail draft migration, simpan SQL, pilih nomor statement, lalu tekan
EXPLAIN (SELECT/INSERT/UPDATE/DELETE) atau EXPLAIN ANALYZE (SELECT baca saja).
Analisis memerlukan View + Edit, memakai revision SQL draft tersimpan dan
transaksi read-only, tidak memberi versi migration atau menulis receipt.
Untuk script beberapa statement, pilih nomor statement; DDL sebelumnya tidak
dijalankan. ANALYZE query mutasi ditolak agar preview tidak memicu sequence,
trigger, atau efek lain yang belum tentu dapat dipulihkan oleh rollback.
Tidak ada perubahan schema metadata atau kunci `.env`; restart server Go setelah
memperbarui kode untuk memakai fitur ini.

## Bonus schema diff dan migration generation

Buka tab **Schema diff** pada database yang ingin diubah. Pilih schema saat ini,
lalu database dan schema pembanding sebagai kondisi yang diinginkan. Pembanding
boleh database lain yang bisa Anda lihat, atau schema lain di database yang sama
(misalnya `public` dibandingkan dengan `rancangan`).

- Halaman menampilkan perbedaan tabel, kolom (tipe, NOT NULL, default), constraint
  (PRIMARY KEY, UNIQUE, CHECK, FOREIGN KEY, EXCLUDE), index, dan view, beserta
  jumlah perubahan yang berisiko menghapus data.
- Kandidat SQL dibuat urut aman: hapus view dan foreign key lama, ubah kolom,
  buat tabel, tambah constraint dan foreign key, lalu index dan view.
- Fungsi dari katalog (misalnya `now()`) ditulis ulang menjadi `pg_catalog.now()`
  dan kandidat langsung diperiksa validator migration yang sama.
- Kandidat ditampilkan di form draft migration yang bisa diedit. SQL baru
  tersimpan setelah user menekan **Simpan sebagai draft**, lalu dijalankan dari
  detail migration seperti draft biasa (bisa dianalisis dengan EXPLAIN dulu).
- Perbandingan hanya membaca katalog dalam transaksi read-only dan memerlukan akses
  View pada kedua database; form draft hanya muncul bagi user dengan View + Edit.
- Rename tidak dideteksi (terbaca sebagai hapus lalu tambah). Perubahan identity,
  serial, generated column, sequence, trigger, function, dan materialized view
  tidak dibuat otomatis; halaman memberi peringatan agar ditulis manual.

## Kredit aset tampilan

- Tata letak dan gaya: [Bootstrap Simple Admin Template](https://github.com/alexisbriandev/bootstrap-simple-admin-template) (Apache License 2.0).
- Bootstrap 5.3.3 (MIT), Font Awesome Free 6 solid (CC BY 4.0 / SIL OFL 1.1 / MIT), dan font Lato (SIL OFL 1.1), semuanya diambil dari folder aset template tersebut. Lisensi disertakan di `src/internal/ui/static`.
- Logo aplikasi menggunakan aset **Stiker IF — “IF Tidur, Besok Balik Nubes”** yang disediakan oleh tim.

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
