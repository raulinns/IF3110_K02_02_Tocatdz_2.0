package security

import "errors"

var (
	ErrMalformedRequest        = errors.New("malformed HTTP request")
	ErrUnsupportedMediaType    = errors.New("unsupported request media type")
	ErrPayloadTooLarge         = errors.New("request body too large")
	ErrInvalidInput            = errors.New("invalid input")
	ErrTargetNotReady          = errors.Join(errors.New("target requires restricted role and protected receipts"), ErrInvalidInput)
	ErrInvalidCredentials      = errors.New("invalid credentials")
	ErrUnauthenticated         = errors.New("authentication required")
	ErrForbidden               = errors.New("access denied")
	ErrCSRF                    = errors.New("invalid request origin or CSRF token")
	ErrNotFound                = errors.New("resource not found")
	ErrConflict                = errors.New("concurrent modification")
	ErrUnavailable             = errors.New("dependency unavailable")
	ErrRateLimited             = errors.New("too many attempts")
	ErrMethodNotAllowed        = errors.New("method not allowed")
	ErrResultTooLarge          = errors.New("result exceeds viewer limit")
	ErrPasswordTooShort        = errors.New("password too short")
	ErrPasswordTooLong         = errors.New("password too long")
	ErrPasswordInvalidEncoding = errors.New("invalid password encoding")
	ErrSecretSerialization     = errors.New("secret cannot be serialized")
)
