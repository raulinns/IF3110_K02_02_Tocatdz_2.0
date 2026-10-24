package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tiap test mendapat database baru agar schema dan data milik test lain tidak ikut berubah.
func Database(t *testing.T, environment string) string {
	t.Helper()
	raw := os.Getenv(environment)
	if raw == "" {
		t.Skip(environment + " is not configured")
	}
	server, err := url.Parse(raw)
	if err != nil || server.Host == "" {
		t.Fatal("invalid integration database URL")
	}
	var suffix [10]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "tocatdz_foundation_" + hex.EncodeToString(suffix[:])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("cannot connect to integration PostgreSQL")
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		conn.Close(ctx)
		t.Fatal("cannot create isolated integration database")
	}
	conn.Close(ctx)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, raw)
		if err != nil {
			t.Error("cannot connect for integration cleanup")
			return
		}
		defer conn.Close(ctx)
		var oid uint32
		if err := conn.QueryRow(ctx, "SELECT oid FROM pg_database WHERE datname=$1", name).Scan(&oid); err != nil {
			t.Error("cannot identify isolated integration database")
			return
		}
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("cannot remove isolated integration database")
			return
		}
		for _, prefix := range []string{"tocatdz_exec_", "tocatdz_receipts_"} {
			if _, err := conn.Exec(ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{prefix + strconv.FormatUint(uint64(oid), 10)}.Sanitize()); err != nil {
				t.Error("cannot remove integration target role")
			}
		}
	})
	copy := *server
	copy.Path = "/" + name
	copy.RawPath = ""
	return copy.String()
}
