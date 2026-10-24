package security

import (
	"errors"
	"unicode/utf8"
)

func ValidateNewPassword(password Secret) error {
	value := password.Reveal()
	var reason error
	switch {
	case len(value) > MaxPasswordBytes:
		reason = ErrPasswordTooLong
	case !utf8.ValidString(value):
		reason = ErrPasswordInvalidEncoding
	case utf8.RuneCountInString(value) < MinPasswordRunes:
		reason = ErrPasswordTooShort
	default:
		return nil
	}
	return errors.Join(ErrInvalidInput, reason)
}
