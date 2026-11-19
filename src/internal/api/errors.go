package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// ValidationError contains trusted, user-facing domain messages, never decoder
// or driver errors. Field keys use K1 paths, e.g. batch.key_column.
type ValidationError struct {
	Fields map[string][]string
}

func (*ValidationError) Error() string { return "request validation failed" }
func (*ValidationError) Unwrap() error { return security.ErrInvalidInput }

type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	RequestID string              `json:"request_id"`
	Fields    map[string][]string `json:"fields,omitempty"`
}

// errorDetail is intentionally independent of the HTML HTTPStatus mapper.
// Only allowlisted text reaches the client. Wrapped internal errors stay private.
func errorDetail(err error) (int, ErrorDetail) {
	status, code, message := http.StatusInternalServerError, "internal", "Terjadi kesalahan"
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, security.ErrMalformedRequest):
		status, code, message = 400, "bad_request", "Permintaan JSON, path, atau query tidak valid"
	case errors.Is(err, security.ErrPayloadTooLarge), errors.As(err, &tooLarge):
		status, code, message = 413, "payload_too_large", "Ukuran permintaan terlalu besar"
	case errors.Is(err, security.ErrUnsupportedMediaType):
		status, code, message = 415, "unsupported_media_type", "Gunakan Content-Type application/json dengan UTF-8"
	case errors.Is(err, security.ErrInvalidCredentials):
		status, code, message = 401, "unauthenticated", "Username atau password tidak valid"
	case errors.Is(err, security.ErrUnauthenticated):
		status, code, message = 401, "unauthenticated", "Silakan login kembali"
	case errors.Is(err, security.ErrCSRF):
		status, code, message = 403, "csrf_failed", "Permintaan tidak dapat diverifikasi. Muat ulang halaman lalu coba lagi."
	case errors.Is(err, security.ErrForbidden):
		status, code, message = 403, "forbidden", "Akses ditolak"
	case errors.Is(err, security.ErrNotFound):
		status, code, message = 404, "not_found", "Data tidak ditemukan"
	case errors.Is(err, security.ErrMethodNotAllowed):
		status, code, message = 405, "method_not_allowed", "Metode tidak diizinkan"
	case errors.Is(err, security.ErrConflict):
		status, code, message = 409, "conflict", "Data atau status berubah, silakan muat ulang"
	case errors.Is(err, security.ErrRateLimited):
		status, code, message = 429, "rate_limited", "Terlalu banyak percobaan"
	case errors.Is(err, security.ErrResultTooLarge):
		status, code, message = 422, "result_too_large", "Hasil terlalu besar, kurangi jumlah data yang ditampilkan"
	case errors.Is(err, security.ErrInvalidInput), errors.Is(err, security.ErrPasswordTooShort), errors.Is(err, security.ErrPasswordTooLong), errors.Is(err, security.ErrPasswordInvalidEncoding):
		status, code, message = 422, "validation_failed", "Input tidak valid"
	case errors.Is(err, security.ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message = 503, "unavailable", "Layanan sementara tidak tersedia"
	}
	detail := ErrorDetail{Code: code, Message: message}
	var validation *ValidationError
	if code == "validation_failed" && errors.As(err, &validation) && validation != nil {
		detail.Fields = make(map[string][]string, len(validation.Fields))
		for key, messages := range validation.Fields {
			detail.Fields[key] = append([]string{}, messages...)
		}
	}
	return status, detail
}

// WriteError is usable as security.HTTPMiddlewareConfig.ErrorWriter. An API
// boundary must run first to provide a server-generated request ID.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, detail := errorDetail(err)
	detail.RequestID = security.RequestIDFromContext(r.Context())
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	w.Header().Del("Location")
	w.Header().Set("Cache-Control", "no-store")
	if detail.Code == "rate_limited" {
		w.Header().Set("Retry-After", "60")
	}
	// Fixed scalar/map DTO cannot fail JSON encoding for valid Go values.
	_ = WriteJSON(w, status, ErrorEnvelope{Error: detail})
}
