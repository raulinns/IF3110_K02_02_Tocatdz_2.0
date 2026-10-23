package security

import (
	"context"
	"encoding/base64"
	"testing"
)

func FuzzDigestToken(f *testing.F) {
	f.Add("")
	f.Add(testToken(1).Reveal())
	f.Add("not-a-token")
	f.Fuzz(func(t *testing.T, input string) {
		if _, err := DigestToken(NewSecret(input)); err == nil {
			raw, err := base64.RawURLEncoding.Strict().DecodeString(input)
			if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != input {
				t.Fatal("accepted token was not canonical")
			}
		}
	})
}

func FuzzPasswordPHCParser(f *testing.F) {
	f.Add("")
	f.Add("$argon2id$v=19$m=4294967295,t=2,p=1$bad$bad")
	f.Add(encodeTestPHC(passwordPHC{memory: 19456, iterations: 2, parallelism: 1, salt: make([]byte, 16)}, make([]byte, 32)).Reveal())
	f.Fuzz(func(t *testing.T, input string) {
		params, err := parsePasswordPHC(NewSecret(input))
		if err == nil && (params.memory < 19456 || params.memory > 65536 || params.iterations < 2 || params.iterations > 4 || params.parallelism < 1 || params.parallelism > 4 || len(params.salt) != 16 || len(params.key) != 32) {
			t.Fatal("unbounded PHC parameters accepted")
		}
	})
}

func FuzzSealedTokenParser(f *testing.F) {
	p, _ := NewAESGCMTokenProtector(testKey(1))
	sealed, _ := p.Seal(context.Background(), testToken(2), testSessionID, 1)
	f.Add("")
	f.Add("v1.not-valid")
	f.Add(sealed.Reveal())
	f.Fuzz(func(t *testing.T, input string) {
		token, err := p.Open(context.Background(), NewSecret(input), testSessionID, 1)
		if err == nil && token.Reveal() != testToken(2).Reveal() {
			t.Fatal("unauthenticated ciphertext accepted")
		}
	})
}
