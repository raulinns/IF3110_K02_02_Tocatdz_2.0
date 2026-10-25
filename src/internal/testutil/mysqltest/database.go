package mysqltest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func Database(t *testing.T, environment string) string {
	t.Helper()
	raw := os.Getenv(environment)
	if raw == "" {
		t.Skip(environment + " is not configured")
	}
	cfg, err := mysqldriver.ParseDSN(raw)
	if err != nil {
		t.Fatal("invalid MySQL integration DSN")
	}
	var suffix [10]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "tocatdz_test_" + hex.EncodeToString(suffix[:])
	serverConfig := *cfg
	serverConfig.DBName = ""
	server, err := sql.Open("mysql", serverConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := server.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		server.Close()
		t.Fatal("cannot create isolated MySQL database")
	}
	server.Close()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server, err := sql.Open("mysql", serverConfig.FormatDSN())
		if err != nil {
			t.Error("cannot open MySQL integration cleanup connection")
			return
		}
		defer server.Close()
		if _, err := server.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
			t.Error("cannot remove isolated MySQL database")
		}
	})
	cfg.DBName = name
	return cfg.FormatDSN()
}
