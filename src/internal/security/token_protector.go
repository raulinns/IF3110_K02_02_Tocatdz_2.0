package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"io"
	"strings"
)

const sealedTokenPrefix = "v1."

// Ganti key sebelum total enkripsi dengan key yang sama mencapai 2^32 di semua instance
type AESGCMTokenProtector struct {
	aead    cipher.AEAD
	entropy io.Reader
}

var _ TokenProtector = (*AESGCMTokenProtector)(nil)

func NewAESGCMTokenProtector(key Secret) (*AESGCMTokenProtector, error) {
	raw, err := decodeKey(key)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, ErrInvalidInput
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &AESGCMTokenProtector{aead: aead, entropy: rand.Reader}, nil
}

func tokenAAD(id ID, generation uint64) ([]byte, error) {
	if !ValidID(id) || generation == 0 {
		return nil, ErrInvalidInput
	}
	aad := append([]byte("tocatdz/session-token/v1\x00"), []byte(id)...)
	return binary.BigEndian.AppendUint64(aad, generation), nil
}

func (p *AESGCMTokenProtector) Seal(ctx context.Context, token Secret, id ID, generation uint64) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	if p == nil || p.aead == nil || p.entropy == nil {
		return Secret{}, ErrInvalidInput
	}
	aad, err := tokenAAD(id, generation)
	if err != nil {
		return Secret{}, err
	}
	raw, err := decodeToken(token)
	if err != nil {
		return Secret{}, ErrInvalidInput
	}
	defer clear(raw)
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(p.entropy, nonce); err != nil {
		return Secret{}, ErrUnavailable
	}
	payload := p.aead.Seal(nonce, nonce, raw, aad)
	return NewSecret(sealedTokenPrefix + base64.RawURLEncoding.EncodeToString(payload)), nil
}

func (p *AESGCMTokenProtector) Open(ctx context.Context, sealed Secret, id ID, generation uint64) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	if p == nil || p.aead == nil {
		return Secret{}, ErrInvalidInput
	}
	aad, err := tokenAAD(id, generation)
	if err != nil {
		return Secret{}, err
	}
	value := sealed.Reveal()
	payloadSize := p.aead.NonceSize() + SessionTokenBytes + p.aead.Overhead()
	if !strings.HasPrefix(value, sealedTokenPrefix) || len(value) != len(sealedTokenPrefix)+base64.RawURLEncoding.EncodedLen(payloadSize) {
		return Secret{}, ErrUnauthenticated
	}
	encoded := strings.TrimPrefix(value, sealedTokenPrefix)
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) != payloadSize || base64.RawURLEncoding.EncodeToString(payload) != encoded {
		return Secret{}, ErrUnauthenticated
	}
	nonce := payload[:p.aead.NonceSize()]
	raw, err := p.aead.Open(nil, nonce, payload[p.aead.NonceSize():], aad)
	if err != nil || len(raw) != SessionTokenBytes {
		return Secret{}, ErrUnauthenticated
	}
	defer clear(raw)
	return NewSecret(base64.RawURLEncoding.EncodeToString(raw)), nil
}
