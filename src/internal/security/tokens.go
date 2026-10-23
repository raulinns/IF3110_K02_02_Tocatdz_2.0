package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
)

type TokenGenerator struct{ entropy io.Reader }

func NewTokenGenerator() *TokenGenerator { return &TokenGenerator{} }

func (g *TokenGenerator) reader() io.Reader {
	if g != nil && g.entropy != nil {
		return g.entropy
	}
	return rand.Reader
}

func (g *TokenGenerator) Generate() (Secret, error) {
	var raw [SessionTokenBytes]byte
	if _, err := io.ReadFull(g.reader(), raw[:]); err != nil {
		return Secret{}, ErrUnavailable
	}
	return NewSecret(base64.RawURLEncoding.EncodeToString(raw[:])), nil
}

func (g *TokenGenerator) GenerateID() (ID, error) {
	var raw [16]byte
	if _, err := io.ReadFull(g.reader(), raw[:]); err != nil {
		return "", ErrUnavailable
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := hex.EncodeToString(raw[:])
	return ID(encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]), nil
}

func decodeToken(token Secret) ([]byte, error) {
	value := token.Reveal()
	if len(value) != base64.RawURLEncoding.EncodedLen(SessionTokenBytes) {
		return nil, ErrUnauthenticated
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != SessionTokenBytes || base64.RawURLEncoding.EncodeToString(raw) != value {
		return nil, ErrUnauthenticated
	}
	return raw, nil
}

func DigestToken(token Secret) (TokenDigest, error) {
	raw, err := decodeToken(token)
	if err != nil {
		return TokenDigest{}, err
	}
	return TokenDigest(sha256.Sum256(raw)), nil
}

func ValidID(id ID) bool {
	value := string(id)
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 || strings.ToLower(compact) != compact {
		return false
	}
	_, err := hex.DecodeString(compact)
	return err == nil
}

func decodeKey(key Secret) ([]byte, error) {
	value := key.Reveal()
	if len(value) != base64.StdEncoding.EncodedLen(32) {
		return nil, ErrInvalidInput
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != 32 || base64.StdEncoding.EncodeToString(raw) != value {
		return nil, ErrInvalidInput
	}
	return raw, nil
}
