package postgres

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const secretBoxVersion byte = 1

// SecretBox mengenkripsi connection string database target dengan AES-256-GCM; AAD mengikat ciphertext ke ID database.
type SecretBox struct {
	aead       cipher.AEAD
	entropy    io.Reader
	receiptKey security.Secret
}

func NewSecretBox(key security.Secret) (*SecretBox, error) {
	value := key.Reveal()
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != 32 || base64.StdEncoding.EncodeToString(raw) != value {
		return nil, security.ErrInvalidInput
	}
	defer clear(raw)
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, security.ErrInvalidInput
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, security.ErrUnavailable
	}
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("tocatdz/receipt/key/v1"))
	return &SecretBox{aead: aead, entropy: rand.Reader, receiptKey: security.NewSecret(string(mac.Sum(nil)))}, nil
}

func (b *SecretBox) Seal(id security.ID, plaintext security.Secret) ([]byte, error) {
	if b == nil || !security.ValidID(id) || plaintext.Empty() {
		return nil, security.ErrInvalidInput
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(b.entropy, nonce); err != nil {
		return nil, security.ErrUnavailable
	}
	out := append([]byte{secretBoxVersion}, nonce...)
	return b.aead.Seal(out, nonce, []byte(plaintext.Reveal()), []byte(id)), nil
}

func (b *SecretBox) Open(id security.ID, sealed []byte) (security.Secret, error) {
	if b == nil {
		return security.Secret{}, security.ErrUnavailable
	}
	size := 1 + b.aead.NonceSize()
	if len(sealed) <= size+b.aead.Overhead() || sealed[0] != secretBoxVersion {
		return security.Secret{}, security.ErrUnavailable
	}
	plain, err := b.aead.Open(nil, sealed[1:size], sealed[size:], []byte(id))
	if err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	defer clear(plain)
	return security.NewSecret(string(plain)), nil
}
