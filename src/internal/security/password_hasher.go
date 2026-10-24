package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Isi fungsi ini pke argon2.IDKey saat merangkai dependency aplikasi
type Argon2idKeyDeriver func(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte

// Pakai satu instance hasher bareng agar maksimal cuma dua proses hashing yang berjalan sekaligus.
type Argon2idHasher struct {
	derive  Argon2idKeyDeriver
	slots   chan struct{}
	entropy io.Reader
}

var _ PasswordHasher = (*Argon2idHasher)(nil)

func NewArgon2idHasher(derive Argon2idKeyDeriver) (*Argon2idHasher, error) {
	if derive == nil {
		return nil, ErrInvalidInput
	}
	return &Argon2idHasher{derive: derive, slots: make(chan struct{}, 2), entropy: rand.Reader}, nil
}

type passwordPHC struct {
	memory, iterations uint32
	parallelism        uint8
	salt, key          []byte
}

func parsePasswordPHC(encoded Secret) (passwordPHC, error) {
	value := encoded.Reveal()
	if len(value) > 512 {
		return passwordPHC{}, ErrInvalidCredentials
	}
	parts := strings.Split(value, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return passwordPHC{}, ErrInvalidCredentials
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return passwordPHC{}, ErrInvalidCredentials
	}
	names := []string{"m=", "t=", "p="}
	values := [3]uint64{}
	for i, param := range params {
		if !strings.HasPrefix(param, names[i]) {
			return passwordPHC{}, ErrInvalidCredentials
		}
		number := strings.TrimPrefix(param, names[i])
		parsed, err := strconv.ParseUint(number, 10, 32)
		if err != nil || strconv.FormatUint(parsed, 10) != number {
			return passwordPHC{}, ErrInvalidCredentials
		}
		values[i] = parsed
	}
	if values[0] < 19456 || values[0] > 65536 || values[1] < 2 || values[1] > 4 || values[2] < 1 || values[2] > 4 {
		return passwordPHC{}, ErrInvalidCredentials
	}
	decode := func(value string, size int) ([]byte, error) {
		if len(value) != base64.RawStdEncoding.EncodedLen(size) {
			return nil, ErrInvalidCredentials
		}
		raw, err := base64.RawStdEncoding.Strict().DecodeString(value)
		if err != nil || len(raw) != size || base64.RawStdEncoding.EncodeToString(raw) != value {
			return nil, ErrInvalidCredentials
		}
		return raw, nil
	}
	salt, err := decode(parts[4], PasswordSaltBytes)
	if err != nil {
		return passwordPHC{}, err
	}
	key, err := decode(parts[5], PasswordKeyBytes)
	if err != nil {
		return passwordPHC{}, err
	}
	return passwordPHC{uint32(values[0]), uint32(values[1]), uint8(values[2]), salt, key}, nil
}

func (h *Argon2idHasher) key(ctx context.Context, password Secret, params passwordPHC) ([]byte, error) {
	if h == nil || h.derive == nil || h.slots == nil || h.entropy == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	passwordBytes := []byte(password.Reveal())
	defer clear(passwordBytes)
	key := h.derive(passwordBytes, params.salt, params.iterations, params.memory, params.parallelism, PasswordKeyBytes)
	// IDKey tidak bisa dihentikan di tengah proses, jadi hasilnya dibuang kalau request sudah dibatalkan.
	if err := ctx.Err(); err != nil {
		clear(key)
		return nil, err
	}
	if len(key) != PasswordKeyBytes {
		clear(key)
		return nil, ErrUnavailable
	}
	return key, nil
}

func (h *Argon2idHasher) Hash(ctx context.Context, password Secret) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	if h == nil || h.entropy == nil {
		return Secret{}, ErrInvalidInput
	}
	if err := ValidateNewPassword(password); err != nil {
		return Secret{}, err
	}
	salt := make([]byte, PasswordSaltBytes)
	if _, err := io.ReadFull(h.entropy, salt); err != nil {
		return Secret{}, ErrUnavailable
	}
	params := passwordPHC{memory: PasswordMemoryKiB, iterations: PasswordIterations, parallelism: PasswordParallelism, salt: salt}
	key, err := h.key(ctx, password, params)
	if err != nil {
		return Secret{}, err
	}
	defer clear(key)
	return NewSecret(fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", params.memory, params.iterations, params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))), nil
}

func (h *Argon2idHasher) Verify(ctx context.Context, encoded, password Secret) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	value := password.Reveal()
	if len(value) > MaxPasswordBytes || !utf8.ValidString(value) {
		return false, ErrInvalidCredentials
	}
	params, err := parsePasswordPHC(encoded)
	if err != nil {
		return false, err
	}
	key, err := h.key(ctx, password, params)
	if err != nil {
		return false, err
	}
	defer clear(key)
	if subtle.ConstantTimeCompare(key, params.key) != 1 {
		return false, ErrInvalidCredentials
	}
	// Jangan turunkan kekuatan hash yang sudah lebih tinggi dari pengaturan saat ini.
	return params.memory < PasswordMemoryKiB || params.iterations < PasswordIterations, nil
}
