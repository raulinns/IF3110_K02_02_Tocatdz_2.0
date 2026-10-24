package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// TEST_DATABASE_URL menunjuk server PostgreSQL khusus test (mis. postgres://postgres@localhost:55432/postgres).
// Setiap test membuat database baru dan menghapusnya; database milik developer tidak pernah disentuh.
func testServer(t *testing.T) *url.URL {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL tidak diset; integration test PostgreSQL dilewati")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL tidak valid")
	}
	return parsed
}

func createDatabase(t *testing.T) string {
	t.Helper()
	server := testServer(t)
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "tocatdz_test_" + hex.EncodeToString(suffix)
	admin, err := pgx.Connect(context.Background(), server.String())
	if err != nil {
		t.Fatal("cannot connect to TEST_DATABASE_URL")
	}
	defer admin.Close(context.Background())
	if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), server.String())
		if err != nil {
			return
		}
		defer conn.Close(context.Background())
		_, _ = conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	target := *server
	target.Path = "/" + name
	return target.String()
}

func testPlatform(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := createDatabase(t)
	pool, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Now().UTC().Truncate(time.Second)}
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *testClock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

func testKey(fill byte) security.Secret {
	return security.NewSecret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

func newID(t *testing.T) security.ID {
	t.Helper()
	id, err := security.NewTokenGenerator().GenerateID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func insertUser(t *testing.T, pool *pgxpool.Pool, username string, platformAdmin bool) security.ID {
	t.Helper()
	id := newID(t)
	exec(t, pool, "INSERT INTO users (id, username, password_hash, platform_admin) VALUES ($1, $2, 'placeholder', $3)", string(id), username, platformAdmin)
	return id
}

func authVersion(t *testing.T, pool *pgxpool.Pool, user security.ID) uint64 {
	t.Helper()
	var version int64
	if err := pool.QueryRow(context.Background(), "SELECT auth_version FROM users WHERE id = $1", string(user)).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return uint64(version)
}
