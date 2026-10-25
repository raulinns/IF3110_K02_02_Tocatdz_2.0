// seed menyiapkan akun dan database demo lokal. Password diambil dari environment, tidak pernah ditulis ke kode.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/bootstrap"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Data contoh di database target supaya explorer dan migration punya isi saat demo.
const sampleTarget = `
CREATE TABLE IF NOT EXISTS public.pelanggan (
	id serial PRIMARY KEY,
	nama text NOT NULL,
	email text NOT NULL UNIQUE,
	dibuat_pada timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS public.pesanan (
	id serial PRIMARY KEY,
	pelanggan_id integer NOT NULL REFERENCES public.pelanggan (id),
	total numeric(12, 2) NOT NULL CHECK (total >= 0),
	status text NOT NULL DEFAULT 'baru'
);
CREATE INDEX IF NOT EXISTS pesanan_pelanggan_idx ON public.pesanan (pelanggan_id);
INSERT INTO public.pelanggan (nama, email) VALUES ('Budi', 'budi@contoh.test'), ('Siti', 'siti@contoh.test'), ('Andi', 'andi@contoh.test')
	ON CONFLICT (email) DO NOTHING;
INSERT INTO public.pesanan (pelanggan_id, total, status)
	SELECT p.id, (p.id * 125000)::numeric, 'baru' FROM public.pelanggan p WHERE NOT EXISTS (SELECT 1 FROM public.pesanan);
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed gagal:", err)
		os.Exit(1)
	}
	fmt.Println("seed selesai")
}

func run() error {
	for _, path := range []string{".env", "../.env"} {
		if err := config.LoadDotEnv(path); err != nil {
			return err
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sec, err := config.LoadSecurity(cfg.AppEnv)
	if err != nil {
		return err
	}
	adminPassword := security.NewSecret(os.Getenv("SEED_ADMIN_PASSWORD"))
	if err := security.ValidateNewPassword(adminPassword); err != nil {
		return errors.New("SEED_ADMIN_PASSWORD wajib diisi, minimal 15 karakter")
	}
	adminUsername, err := security.NormalizeUsername(valueOr(os.Getenv("SEED_ADMIN_USERNAME"), "admin"))
	if err != nil {
		return errors.New("SEED_ADMIN_USERNAME tidak valid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}
	hasher, err := bootstrap.NewPasswordHasher()
	if err != nil {
		return err
	}
	input := postgres.SeedInput{AdminUsername: adminUsername}
	if input.AdminHash, err = hasher.Hash(ctx, adminPassword); err != nil {
		return err
	}
	if viewerPassword := security.NewSecret(os.Getenv("SEED_VIEWER_PASSWORD")); !viewerPassword.Empty() {
		if err := security.ValidateNewPassword(viewerPassword); err != nil {
			return errors.New("SEED_VIEWER_PASSWORD minimal 15 karakter")
		}
		input.ViewerUsername = "pengamat"
		if input.ViewerHash, err = hasher.Hash(ctx, viewerPassword); err != nil {
			return err
		}
	}
	box, err := postgres.NewSecretBox(sec.ConnectionKey)
	if err != nil {
		return err
	}
	if targetURL := strings.TrimSpace(os.Getenv("SEED_TARGET_DATABASE_URL")); targetURL != "" {
		runtimeDSN, err := postgres.PrepareManagedTarget(ctx, pool, security.NewSecret(targetURL), box)
		if err != nil {
			return err
		}
		input.TargetName, input.TargetDSN = valueOr(os.Getenv("SEED_TARGET_NAME"), "demo"), runtimeDSN
		conn, err := pgx.Connect(ctx, runtimeDSN.Reveal())
		if err != nil {
			return errors.New("SEED_TARGET_DATABASE_URL tidak bisa dihubungi")
		}
		_, err = conn.Exec(ctx, sampleTarget)
		conn.Close(ctx)
		if err != nil {
			return errors.New("gagal membuat data contoh di database target")
		}
	}
	return postgres.Seed(ctx, pool, box, security.NewTokenGenerator(), input)
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
