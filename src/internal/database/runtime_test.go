package database

import (
	"bytes"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestNewRuntimeBuildsDatabaseDependencies(t *testing.T) {
	runtime, err := NewRuntime(&pgxpool.Pool{}, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("NewRuntime() returned error: %v", err)
	}
	if runtime.Store == nil || runtime.Catalog == nil || runtime.Connections == nil || runtime.Reader == nil {
		t.Fatalf("NewRuntime() left a dependency nil: %+v", runtime)
	}
	engines := runtime.Registry.Engines()
	if len(engines) != 1 || engines[0] != dbms.PostgreSQL {
		t.Fatalf("registered engines = %v", engines)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

func TestNewRuntimeRejectsInvalidDependencies(t *testing.T) {
	tests := []struct {
		name string
		pool *pgxpool.Pool
		key  []byte
	}{
		{name: "nil platform pool", key: bytes.Repeat([]byte{7}, 32)},
		{name: "invalid encryption key", pool: &pgxpool.Pool{}, key: []byte("short")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRuntime(tt.pool, tt.key)
			if !errors.Is(err, security.ErrInvalidInput) {
				t.Fatalf("NewRuntime() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}
