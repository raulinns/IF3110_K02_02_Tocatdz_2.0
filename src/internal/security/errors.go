package security

import "errors"

var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrUnauthenticated     = errors.New("authentication required")
	ErrForbidden           = errors.New("access denied")
	ErrCSRF                = errors.New("invalid request origin or CSRF token")
	ErrNotFound            = errors.New("resource not found")
	ErrConflict            = errors.New("concurrent modification")
	ErrUnavailable         = errors.New("dependency unavailable")
	ErrRateLimited         = errors.New("too many attempts")
	ErrSecretSerialization = errors.New("secret cannot be serialized")
)
