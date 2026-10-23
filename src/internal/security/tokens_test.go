package security

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
)

const (
	testUserID    ID = "b0c4f0c0-9167-43d6-9089-afd7b9826f0d"
	testSessionID ID = "5ed48df2-bcdd-4b52-b9ad-8d9f6689df55"
	otherID       ID = "4edd2fdc-1aab-46fc-b9bf-ff1f16a8a1e6"
)

type brokenEntropy struct{}

func (brokenEntropy) Read([]byte) (int, error) { return 0, errors.New("private entropy failure") }

func testKey(fill byte) Secret {
	return NewSecret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

func testToken(fill byte) Secret {
	return NewSecret(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

func testPrincipal() Principal {
	return Principal{UserID: testUserID, SessionID: testSessionID, AuthVersion: 1, CSRFEpoch: 1}
}

func TestTokenGenerationAndDigest(t *testing.T) {
	g := NewTokenGenerator()
	seen := map[TokenDigest]bool{}
	for range 100 {
		token, err := g.Generate()
		if err != nil {
			t.Fatal(err)
		}
		digest, err := DigestToken(token)
		if err != nil || seen[digest] {
			t.Fatal("token invalid or repeated")
		}
		seen[digest] = true
		raw, _ := base64.RawURLEncoding.DecodeString(token.Reveal())
		if digest != TokenDigest(sha256.Sum256(raw)) {
			t.Fatal("digest must hash decoded bytes")
		}
		id, err := g.GenerateID()
		if err != nil || !ValidID(id) || id[14] != '4' || !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatal("not a canonical UUID v4")
		}
	}
}

func TestTokenInputRejected(t *testing.T) {
	for _, value := range []string{"", "abc", strings.Repeat("A", 44), strings.Repeat("A", 42) + "B", strings.Repeat("A", 42) + "=", strings.Repeat("A", 42) + "/", testToken(1).Reveal() + "\n", strings.Repeat("A", 4096)} {
		if _, err := DigestToken(NewSecret(value)); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("malformed token accepted (length %d)", len(value))
		}
	}
	for _, id := range []ID{"", "user", "00000000-0000-0000-0000-000000000000", ID(strings.ToUpper(string(testSessionID))), "5ed48df2-bcdd-4b52-b9ad-8d9f6689df5z"} {
		if ValidID(id) {
			t.Error("malformed/zero ID accepted")
		}
	}
}

func TestTokenEntropyFailure(t *testing.T) {
	for _, entropy := range []io.Reader{brokenEntropy{}, bytes.NewReader([]byte{1})} {
		g := &TokenGenerator{entropy: entropy}
		if token, err := g.Generate(); !errors.Is(err, ErrUnavailable) || !token.Empty() {
			t.Fatal("token generation did not fail closed")
		}
		if id, err := g.GenerateID(); !errors.Is(err, ErrUnavailable) || id != "" {
			t.Fatal("UUID generation did not fail closed")
		}
	}
}
