package security

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidateNewPasswordReportsSafeSpecificReasons(t *testing.T) {
	for _, test := range []struct {
		name, input string
		reason      error
	}{
		{"empty", "", ErrPasswordTooShort},
		{"short", strings.Repeat("a", MinPasswordRunes-1), ErrPasswordTooShort},
		{"short unicode", strings.Repeat("界", MinPasswordRunes-1), ErrPasswordTooShort},
		{"long", strings.Repeat("a", MaxPasswordBytes+1), ErrPasswordTooLong},
		{"long unicode", strings.Repeat("界", 342), ErrPasswordTooLong},
		{"encoding", "private-canary-" + string([]byte{0xff}), ErrPasswordInvalidEncoding},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateNewPassword(NewSecret(test.input))
			if !errors.Is(err, test.reason) || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("wrong error category: %v", err)
			}
			if strings.Contains(err.Error(), "private-canary") {
				t.Fatal("validation error contains password")
			}
			if status, _ := HTTPStatus(err); status != 400 {
				t.Fatal("validation error lost HTTP input category")
			}
		})
	}
	for _, input := range []string{strings.Repeat("a", 15), strings.Repeat("界", 15), strings.Repeat(" ", 15), strings.Repeat("a", MaxPasswordBytes)} {
		if err := ValidateNewPassword(NewSecret(input)); err != nil {
			t.Fatal("valid password rejected or normalized")
		}
	}
}

func TestPasswordHashUsesSharedValidatorWithoutCallingKDF(t *testing.T) {
	calls := 0
	hasher, _ := NewArgon2idHasher(func([]byte, []byte, uint32, uint32, uint8, uint32) []byte { calls++; return make([]byte, 32) })
	for _, test := range []struct {
		input  string
		reason error
	}{
		{"short", ErrPasswordTooShort}, {strings.Repeat("x", MaxPasswordBytes+1), ErrPasswordTooLong}, {string([]byte{0xff}), ErrPasswordInvalidEncoding},
	} {
		_, err := hasher.Hash(context.Background(), NewSecret(test.input))
		if !errors.Is(err, test.reason) || !errors.Is(err, ErrInvalidInput) {
			t.Fatal("Hash diverged from shared validation")
		}
	}
	if calls != 0 {
		t.Fatal("invalid new password invoked expensive KDF")
	}
}
