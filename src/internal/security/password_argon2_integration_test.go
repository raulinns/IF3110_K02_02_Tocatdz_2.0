//go:build argon2integration

package security_test

import (
	"context"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"golang.org/x/crypto/argon2"
)

// Jalankan dengan -tags=argon2integration untuk menguji backend Argon2id asli.
func TestArgon2idBackendIntegration(t *testing.T) {
	hasher, err := security.NewArgon2idHasher(argon2.IDKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	password := security.NewSecret("a sufficiently long passphrase  ")
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	if rehash, err := hasher.Verify(ctx, hash, password); err != nil || rehash {
		t.Fatal("real Argon2id roundtrip failed")
	}
	if _, err := hasher.Verify(ctx, hash, security.NewSecret("a sufficiently long passphrase")); !errors.Is(err, security.ErrInvalidCredentials) {
		t.Fatal("wrong password accepted by actual backend")
	}
}
