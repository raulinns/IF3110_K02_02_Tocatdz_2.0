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
	for _, account := range []struct {
		env, username string
		hash          *security.Secret
		name          *string
	}{
		{"SEED_VIEWER_PASSWORD", "pengamat", &input.ViewerHash, &input.ViewerUsername},
		{"SEED_EDITOR_PASSWORD", "editor", &input.EditorHash, &input.EditorUsername},
	} {
		password := security.NewSecret(os.Getenv(account.env))
		if password.Empty() {
			continue
		}
		if err := security.ValidateNewPassword(password); err != nil {
			return errors.New(account.env + " minimal 15 karakter")
		}
		*account.name = account.username
		if *account.hash, err = hasher.Hash(ctx, password); err != nil {
			return err
		}
	}
	box, err := postgres.NewSecretBox(sec.ConnectionKey)
	if err != nil {
		return err
	}
	targets := []struct{ urlEnv, nameEnv, fallback string }{
		{"SEED_TARGET_DATABASE_URL", "SEED_TARGET_NAME", "demo"},
		{"SEED_TARGET2_DATABASE_URL", "SEED_TARGET2_NAME", "gudang"},
	}
	seeded := false
	for _, target := range targets {
		targetURL := strings.TrimSpace(os.Getenv(target.urlEnv))
		if targetURL == "" {
			continue
		}
		runtimeDSN, err := postgres.PrepareManagedTarget(ctx, pool, security.NewSecret(targetURL), box)
		if err != nil {
			return err
		}
		conn, err := pgx.Connect(ctx, runtimeDSN.Reveal())
		if err != nil {
			return errors.New(target.urlEnv + " tidak bisa dihubungi")
		}
		_, err = conn.Exec(ctx, sampleTarget)
		conn.Close(ctx)
		if err != nil {
			return errors.New("gagal membuat data contoh di database target")
		}
		input.TargetName, input.TargetDSN = valueOr(os.Getenv(target.nameEnv), target.fallback), runtimeDSN
		if err := postgres.Seed(ctx, pool, box, security.NewTokenGenerator(), input); err != nil {
			return err
		}
		seeded = true
	}
	if seeded {
		return nil
	}
	return postgres.Seed(ctx, pool, box, security.NewTokenGenerator(), input)
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
