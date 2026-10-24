package security

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// stubKey cm buat unit test, sedangkan integration test memakai Argon2id asli.
func stubKey(password, salt []byte, iterations, memory uint32, parallelism uint8, keyLen uint32) []byte {
	input := append(append([]byte(nil), password...), salt...)
	key := sha256.Sum256(input)
	return key[:]
}

func encodeTestPHC(params passwordPHC, key []byte) Secret {
	return NewSecret(fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", params.memory, params.iterations, params.parallelism,
		base64.RawStdEncoding.EncodeToString(params.salt), base64.RawStdEncoding.EncodeToString(key)))
}

func TestStrongerAcceptedPHCIsNotDowngraded(t *testing.T) {
	h, _ := NewArgon2idHasher(stubKey)
	params := passwordPHC{memory: 65536, iterations: 4, parallelism: 4, salt: make([]byte, 16)}
	key := stubKey([]byte("test password"), params.salt, params.iterations, params.memory, params.parallelism, 32)
	if rehash, err := h.Verify(context.Background(), encodeTestPHC(params, key), NewSecret("test password")); err != nil || rehash {
		t.Fatal("stronger accepted hash was rejected or marked for downgrade")
	}
}

func TestPasswordPHCRoundTripAndFreshSalt(t *testing.T) {
	var calls int
	h, err := NewArgon2idHasher(func(password, salt []byte, iterations, memory uint32, parallelism uint8, keyLen uint32) []byte {
		calls++
		if iterations != PasswordIterations || memory != PasswordMemoryKiB || parallelism != PasswordParallelism || keyLen != PasswordKeyBytes || len(salt) != PasswordSaltBytes {
			t.Fatal("wrong Argon2id arguments")
		}
		return stubKey(password, salt, iterations, memory, parallelism, keyLen)
	})
	if err != nil {
		t.Fatal(err)
	}
	password := NewSecret("a secure passphrase with spaces  ")
	first, err := h.Hash(context.Background(), password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Hash(context.Background(), password)
	if err != nil || first.Reveal() == second.Reveal() {
		t.Fatal("password salts reused")
	}
	if strings.Contains(first.Reveal(), password.Reveal()) || !strings.HasPrefix(first.Reveal(), "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal("bad PHC encoding")
	}
	if rehash, err := h.Verify(context.Background(), first, password); err != nil || rehash {
		t.Fatal("good password rejected")
	}
	for _, wrong := range []Secret{NewSecret(strings.TrimSpace(password.Reveal())), NewSecret("wrong"), {}} {
		if _, err := h.Verify(context.Background(), first, wrong); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("wrong password accepted or normalized")
		}
	}
	if calls != 6 {
		t.Fatalf("expected KDF calls, got %d", calls)
	}
}

func TestPasswordInputPolicy(t *testing.T) {
	h, _ := NewArgon2idHasher(stubKey)
	for _, password := range []string{"", "short", strings.Repeat("x", MaxPasswordBytes+1), string([]byte{0xff})} {
		if value, err := h.Hash(context.Background(), NewSecret(password)); !errors.Is(err, ErrInvalidInput) || !value.Empty() {
			t.Fatal("invalid new password accepted")
		}
	}
	for _, password := range []string{strings.Repeat("界", 15), strings.Repeat(" ", 15), strings.Repeat("x", MaxPasswordBytes)} {
		if _, err := h.Hash(context.Background(), NewSecret(password)); err != nil {
			t.Fatal("valid password changed/rejected")
		}
	}
	params := passwordPHC{memory: PasswordMemoryKiB, iterations: 2, parallelism: 1, salt: make([]byte, 16)}
	key := stubKey([]byte("old"), params.salt, 2, PasswordMemoryKiB, 1, 32)
	encoded := encodeTestPHC(params, key)
	if _, err := h.Verify(context.Background(), encoded, NewSecret("old")); err != nil {
		t.Fatal("existing short password rejected")
	}
	if _, err := h.Verify(context.Background(), encoded, NewSecret(strings.Repeat("x", MaxPasswordBytes+1))); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("oversized verify accepted")
	}
}

func TestMalformedPHCNeverInvokesKDF(t *testing.T) {
	calls := 0
	h, _ := NewArgon2idHasher(func([]byte, []byte, uint32, uint32, uint8, uint32) []byte { calls++; return make([]byte, 32) })
	valid, _ := h.Hash(context.Background(), NewSecret("a valid test passphrase"))
	calls = 0
	for _, phc := range []string{
		"", "plaintext", strings.Repeat("x", 513),
		strings.Replace(valid.Reveal(), "argon2id", "argon2i", 1),
		strings.Replace(valid.Reveal(), "v=19", "v=16", 1),
		strings.Replace(valid.Reveal(), "m=19456", "m=4294967295", 1),
		strings.Replace(valid.Reveal(), "m=19456", "m=19455", 1),
		strings.Replace(valid.Reveal(), "m=19456", "m=019456", 1),
		strings.Replace(valid.Reveal(), "t=2", "t=5", 1),
		strings.Replace(valid.Reveal(), "p=1", "p=0", 1),
		strings.Replace(valid.Reveal(), "p=1", "p=5", 1),
		valid.Reveal() + "=", valid.Reveal() + "$extra", valid.Reveal() + "\n",
	} {
		if _, err := h.Verify(context.Background(), NewSecret(phc), NewSecret("test")); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("unsafe PHC accepted")
		}
	}
	if calls != 0 {
		t.Fatal("untrusted PHC reached expensive KDF")
	}
}

func TestPasswordFailuresAndCancellation(t *testing.T) {
	if _, err := NewArgon2idHasher(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing Argon2 dependency accepted")
	}
	h, _ := NewArgon2idHasher(stubKey)
	h.entropy = brokenEntropy{}
	if value, err := h.Hash(context.Background(), NewSecret("a valid test passphrase")); !errors.Is(err, ErrUnavailable) || !value.Empty() {
		t.Fatal("salt failure accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Hash(ctx, NewSecret("a valid test passphrase")); !errors.Is(err, context.Canceled) {
		t.Fatal("Hash ignored cancellation")
	}
	if _, err := h.Verify(ctx, Secret{}, Secret{}); !errors.Is(err, context.Canceled) {
		t.Fatal("Verify ignored cancellation")
	}
	broken, _ := NewArgon2idHasher(func([]byte, []byte, uint32, uint32, uint8, uint32) []byte { return nil })
	if _, err := broken.Hash(context.Background(), NewSecret("a valid test passphrase")); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid KDF output accepted")
	}
}

func TestHasherLimitsConcurrencyAndHonorsWaitingCancellation(t *testing.T) {
	var active atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	h, _ := NewArgon2idHasher(func(password, salt []byte, iterations, memory uint32, parallelism uint8, keyLen uint32) []byte {
		if active.Add(1) > 2 {
			t.Error("too many simultaneous KDF allocations")
		}
		defer active.Add(-1)
		entered <- struct{}{}
		<-release
		return make([]byte, 32)
	})
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := h.Hash(context.Background(), NewSecret("a valid test passphrase")); results <- err }()
	}
	<-entered
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { _, err := h.Hash(ctx, NewSecret("a valid test passphrase")); waiting <- err }()
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Error("waiting KDF ignored cancellation")
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
