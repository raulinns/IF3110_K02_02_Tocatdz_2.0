// Command seed-large menambah row public.orders sampai UPDATE seluruh row tanpa batching > 1 detik.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
)

func main() {
	start := flag.Int64("start", 250_000, "initial number of rows")
	maxRows := flag.Int64("max", 20_000_000, "maximum number of rows")
	minMS := flag.Int64("min-ms", 1000, "required duration of the full-table UPDATE in milliseconds")
	flag.Parse()
	if err := run(*start, *maxRows, time.Duration(*minMS)*time.Millisecond); err != nil {
		fmt.Fprintln(os.Stderr, "seed-large failed:", err)
		os.Exit(1)
	}
}

func run(start, maxRows int64, minimum time.Duration) error {
	for _, path := range []string{".env", "../.env"} {
		if err := config.LoadDotEnv(path); err != nil {
			return err
		}
	}
	dsn := os.Getenv("TARGET_ADMIN_DATABASE_URL")
	if dsn == "" {
		return errors.New("TARGET_ADMIN_DATABASE_URL is required (admin DSN of the dedicated target database)")
	}
	if start < 1 || maxRows < start || minimum <= 0 {
		return errors.New("invalid flags")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return errors.New("cannot connect to target database")
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.orders (
		id bigint PRIMARY KEY, customer_id integer NOT NULL, amount numeric(12,2) NOT NULL, status text NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	// Tabel harus dimiliki role runtime agar bisa dimigrasi.
	var oid uint32
	if err := conn.QueryRow(ctx, "SELECT oid FROM pg_database WHERE datname = current_database()").Scan(&oid); err != nil {
		return err
	}
	runtime := "tocatdz_exec_" + strconv.FormatUint(uint64(oid), 10)
	var present bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", runtime).Scan(&present); err != nil {
		return err
	}
	if present {
		if _, err := conn.Exec(ctx, "ALTER TABLE public.orders OWNER TO "+pgx.Identifier{runtime}.Sanitize()); err != nil {
			return fmt.Errorf("transfer ownership: %w", err)
		}
	}
	target := start
	for {
		if err := grow(ctx, conn, target); err != nil {
			return err
		}
		rows, duration, err := benchmark(ctx, conn)
		if err != nil {
			return err
		}
		fmt.Printf("rows=%d full_update_ms=%d\n", rows, duration.Milliseconds())
		if duration > minimum {
			fmt.Printf("RESULT: %d rows, UPDATE without batching took %d ms (> %d ms). Use this dataset for the batch test.\n",
				rows, duration.Milliseconds(), minimum.Milliseconds())
			return nil
		}
		if target >= maxRows {
			return fmt.Errorf("UPDATE stayed below %d ms at %d rows; raise -max", minimum.Milliseconds(), rows)
		}
		target = min(target*2, maxRows)
	}
}

func grow(ctx context.Context, conn *pgx.Conn, target int64) error {
	var current int64
	if err := conn.QueryRow(ctx, "SELECT COALESCE(max(id), 0) FROM public.orders").Scan(&current); err != nil {
		return err
	}
	if current >= target {
		return nil
	}
	_, err := conn.Exec(ctx, `INSERT INTO public.orders (id, customer_id, amount, status)
		SELECT g, (g % 10000)::int, round((random() * 1000)::numeric, 2), CASE WHEN g % 3 = 0 THEN 'paid' ELSE 'pending' END
		FROM generate_series($1::bigint, $2::bigint) AS g`, current+1, target)
	if err != nil {
		return fmt.Errorf("insert rows: %w", err)
	}
	_, err = conn.Exec(ctx, "ANALYZE public.orders")
	return err
}

// benchmark di-rollback sehingga data tidak berubah.
func benchmark(ctx context.Context, conn *pgx.Conn) (int64, time.Duration, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	started := time.Now()
	tag, err := tx.Exec(ctx, "UPDATE public.orders SET amount = amount * 1.01, updated_at = now()")
	if err != nil {
		return 0, 0, fmt.Errorf("benchmark update: %w", err)
	}
	return tag.RowsAffected(), time.Since(started), nil
}
