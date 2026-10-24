package ui

import (
	"context"
	"errors"
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type ErrorView struct {
	Status  int
	Message string
}

// Sama dengan security.WriteHTTPError
func errorResponse(err error) (int, string) {
	switch {
	case errors.Is(err, security.ErrInvalidInput):
		return http.StatusBadRequest, "Input tidak valid"
	case errors.Is(err, security.ErrInvalidCredentials):
		return http.StatusUnauthorized, "Username atau password tidak valid"
	case errors.Is(err, security.ErrUnauthenticated):
		return http.StatusUnauthorized, "Silakan login kembali"
	case errors.Is(err, security.ErrForbidden):
		return http.StatusForbidden, "Akses ditolak"
	case errors.Is(err, security.ErrCSRF):
		return http.StatusForbidden, "Permintaan tidak valid"
	case errors.Is(err, security.ErrNotFound):
		return http.StatusNotFound, "Data tidak ditemukan"
	case errors.Is(err, security.ErrConflict):
		return http.StatusConflict, "Data berubah, silakan muat ulang"
	case errors.Is(err, security.ErrRateLimited):
		return http.StatusTooManyRequests, "Terlalu banyak percobaan"
	case errors.Is(err, security.ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, "Layanan sementara tidak tersedia"
	}
	return http.StatusInternalServerError, "Terjadi kesalahan"
}

func (r *Renderer) RenderError(w http.ResponseWriter, req *http.Request, err error) error {
	status, message := errorResponse(err)
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "60")
	}
	return r.Render(w, req, status, "error", Page{Title: message, Content: ErrorView{Status: status, Message: message}})
}
