package dbms

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestConnectionProtectorRoundTripIsBoundToDatabase(t *testing.T) {
	protector, err := NewConnectionProtector(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("NewConnectionProtector() returned error: %v", err)
	}
	protector.entropy = bytes.NewReader(bytes.Repeat([]byte{3}, protector.aead.NonceSize()))

	databaseID := security.ID("11111111-1111-4111-8111-111111111111")
	dsn := security.NewSecret("postgres://app:private-password@db:5432/inventory")
	ciphertext, err := protector.Seal(context.Background(), databaseID, dsn)
	if err != nil {
		t.Fatalf("Seal() returned error: %v", err)
	}
	if bytes.Contains(ciphertext, []byte("private-password")) {
		t.Fatal("ciphertext contains plaintext credentials")
	}
	opened, err := protector.Open(context.Background(), databaseID, ciphertext)
	if err != nil || opened.Reveal() != dsn.Reveal() {
		t.Fatalf("Open() did not recover the original DSN: %v", err)
	}

	otherID := security.ID("22222222-2222-4222-8222-222222222222")
	if _, err := protector.Open(context.Background(), otherID, ciphertext); !errors.Is(err, security.ErrUnavailable) {
		t.Fatalf("Open() with another database ID error = %v, want ErrUnavailable", err)
	}
}

func TestConnectionProtectorRejectsInvalidInputsAndTampering(t *testing.T) {
	if _, err := NewConnectionProtector([]byte("short")); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatalf("short key error = %v, want ErrInvalidInput", err)
	}
	protector, _ := NewConnectionProtector(bytes.Repeat([]byte{9}, 32))
	databaseID := security.ID("11111111-1111-4111-8111-111111111111")
	if _, err := protector.Seal(context.Background(), databaseID, security.Secret{}); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatalf("empty DSN error = %v, want ErrInvalidInput", err)
	}
	if _, err := protector.Open(context.Background(), databaseID, []byte("broken")); !errors.Is(err, security.ErrUnavailable) {
		t.Fatalf("tampered ciphertext error = %v, want ErrUnavailable", err)
	}
}
