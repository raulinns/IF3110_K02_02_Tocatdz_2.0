package security

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestTokenProtectorRoundTripAndNonce(t *testing.T) {
	p, err := NewAESGCMTokenProtector(testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	token := testToken(2)
	first, err := p.Seal(ctx, token, testSessionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Seal(ctx, token, testSessionID, 1)
	if err != nil || first.Reveal() == second.Reveal() || strings.Contains(first.Reveal(), token.Reveal()) {
		t.Fatal("nonce reuse or plaintext disclosure")
	}
	other, _ := NewAESGCMTokenProtector(testKey(1))
	opened, err := other.Open(ctx, first, testSessionID, 1)
	if err != nil || opened.Reveal() != token.Reveal() {
		t.Fatal("persisted token could not be recovered")
	}
}

func TestTokenProtectorRejectsTamperingAndWrongBinding(t *testing.T) {
	ctx := context.Background()
	p, _ := NewAESGCMTokenProtector(testKey(1))
	sealed, _ := p.Seal(ctx, testToken(2), testSessionID, 1)
	encoded := strings.TrimPrefix(sealed.Reveal(), sealedTokenPrefix)
	payload, _ := base64.RawURLEncoding.DecodeString(encoded)
	for _, offset := range []int{0, 12, len(payload) - 1} {
		changed := append([]byte(nil), payload...)
		changed[offset] ^= 1
		bad := NewSecret(sealedTokenPrefix + base64.RawURLEncoding.EncodeToString(changed))
		if token, err := p.Open(ctx, bad, testSessionID, 1); !errors.Is(err, ErrUnauthenticated) || !token.Empty() {
			t.Fatal("tampered nonce/ciphertext/tag accepted")
		}
	}
	for _, binding := range []struct {
		id  ID
		gen uint64
	}{{otherID, 1}, {testSessionID, 2}} {
		if _, err := p.Open(ctx, sealed, binding.id, binding.gen); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("wrong family/generation accepted")
		}
	}
	wrongKey, _ := NewAESGCMTokenProtector(testKey(3))
	if _, err := wrongKey.Open(ctx, sealed, testSessionID, 1); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("wrong key accepted")
	}
	for _, value := range []string{"", "v2." + encoded, "v1.abc", sealed.Reveal() + "\n", strings.Repeat("A", 4096)} {
		if _, err := p.Open(ctx, NewSecret(value), testSessionID, 1); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("invalid envelope accepted")
		}
	}
}

func TestTokenProtectorFailures(t *testing.T) {
	for _, key := range []Secret{{}, NewSecret("short"), NewSecret(testKey(1).Reveal() + "\n"), NewSecret(strings.Repeat("A", 43) + "B")} {
		if _, err := NewAESGCMTokenProtector(key); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid encryption key accepted")
		}
	}
	p, _ := NewAESGCMTokenProtector(testKey(1))
	p.entropy = brokenEntropy{}
	if token, err := p.Seal(context.Background(), testToken(2), testSessionID, 1); !errors.Is(err, ErrUnavailable) || !token.Empty() {
		t.Fatal("entropy failure did not fail closed")
	}
	if _, err := p.Seal(context.Background(), testToken(2), "bad", 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("invalid family accepted")
	}
	if _, err := p.Seal(context.Background(), NewSecret("bad"), testSessionID, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("invalid raw token accepted")
	}
	if _, err := p.Open(context.Background(), Secret{}, testSessionID, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("zero generation accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Seal(ctx, testToken(1), testSessionID, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("Seal ignored cancellation")
	}
	if _, err := p.Open(ctx, Secret{}, testSessionID, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("Open ignored cancellation")
	}
	var zero AESGCMTokenProtector
	if _, err := zero.Seal(context.Background(), testToken(1), testSessionID, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unconfigured encryption accepted")
	}
}

func TestTokenProtectorConcurrentUse(t *testing.T) {
	p, _ := NewAESGCMTokenProtector(testKey(1))
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := NewTokenGenerator().Generate()
			if err != nil {
				t.Error(err)
				return
			}
			sealed, err := p.Seal(context.Background(), token, testSessionID, 1)
			if err != nil {
				t.Error(err)
				return
			}
			got, err := p.Open(context.Background(), sealed, testSessionID, 1)
			if err != nil || got.Reveal() != token.Reveal() {
				t.Error("concurrent recovery failed")
			}
		}()
	}
	wg.Wait()
}
