package dbms

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type ConnectionProtector struct {
	aead    cipher.AEAD
	entropy io.Reader
}

func NewConnectionProtector(key []byte) (*ConnectionProtector, error) {
	if len(key) != 32 {
		return nil, security.ErrInvalidInput
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, security.ErrInvalidInput
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, security.ErrUnavailable
	}
	return &ConnectionProtector{aead: aead, entropy: rand.Reader}, nil
}

func (p *ConnectionProtector) Seal(
	ctx context.Context,
	databaseID security.ID,
	dsn security.Secret,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || p.aead == nil || !security.ValidID(databaseID) || dsn.Empty() {
		return nil, security.ErrInvalidInput
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(p.entropy, nonce); err != nil {
		return nil, security.ErrUnavailable
	}
	return p.aead.Seal(nonce, nonce, []byte(dsn.Reveal()), []byte(databaseID)), nil
}

func (p *ConnectionProtector) Open(
	ctx context.Context,
	databaseID security.ID,
	ciphertext []byte,
) (security.Secret, error) {
	if err := ctx.Err(); err != nil {
		return security.Secret{}, err
	}
	if p == nil || p.aead == nil || !security.ValidID(databaseID) || len(ciphertext) <= p.aead.NonceSize() {
		return security.Secret{}, security.ErrUnavailable
	}
	nonceSize := p.aead.NonceSize()
	plaintext, err := p.aead.Open(
		nil,
		ciphertext[:nonceSize],
		ciphertext[nonceSize:],
		[]byte(databaseID),
	)
	if err != nil || len(plaintext) == 0 {
		return security.Secret{}, security.ErrUnavailable
	}
	return security.NewSecret(string(plaintext)), nil
}
