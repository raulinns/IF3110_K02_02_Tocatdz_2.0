package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func main() {
	output := flag.String("out", "target-runtime.env", "file lokal untuk DSN akun target")
	flag.Parse()
	if err := run(*output); err != nil {
		fmt.Fprintln(os.Stderr, "setup target gagal:", err)
		os.Exit(1)
	}
	fmt.Println("Setup target selesai; salin TARGET_DATABASE_URL dari file output ke form koneksi admin.")
}

func run(output string) error {
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
	adminDSN := security.NewSecret(os.Getenv("TARGET_ADMIN_DATABASE_URL"))
	if adminDSN.Empty() {
		return errors.New("TARGET_ADMIN_DATABASE_URL wajib diisi untuk database target khusus aplikasi")
	}
	// File baru saja yang boleh ditulis supaya kredensial lama tidak tertimpa.
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("file output sudah ada atau tidak dapat dibuat")
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(output)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	box, err := postgres.NewSecretBox(sec.ConnectionKey)
	if err != nil {
		return err
	}
	dsn, err := postgres.PrepareManagedTarget(ctx, pool, adminDSN, box)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(file, "TARGET_DATABASE_URL="+dsn.Reveal()); err != nil {
		return errors.New("gagal menulis file output")
	}
	if err := file.Close(); err != nil {
		return errors.New("gagal menyimpan file output")
	}
	ok = true
	return nil
}
