package security

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

func HTTPStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrTargetNotReady):
		return http.StatusBadRequest, "Koneksi target perlu akun terbatas dan konfigurasi receipt yang sesuai."
	case errors.Is(err, ErrPasswordTooShort):
		return http.StatusBadRequest, fmt.Sprintf("Password minimal %d karakter", MinPasswordRunes)
	case errors.Is(err, ErrPasswordTooLong):
		return http.StatusBadRequest, fmt.Sprintf("Password maksimal %d byte", MaxPasswordBytes)
	case errors.Is(err, ErrPasswordInvalidEncoding):
		return http.StatusBadRequest, "Password harus berupa teks UTF-8 yang valid"
	case errors.Is(err, ErrInvalidInput):
		return http.StatusBadRequest, "Input tidak valid"
	case errors.Is(err, ErrInvalidCredentials):
		return http.StatusUnauthorized, "Username atau password tidak valid"
	case errors.Is(err, ErrUnauthenticated):
		return http.StatusUnauthorized, "Silakan login kembali"
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden, "Akses ditolak"
	case errors.Is(err, ErrCSRF):
		return http.StatusForbidden, "Permintaan tidak dapat diverifikasi. Muat ulang halaman lalu coba lagi."
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "Data tidak ditemukan"
	case errors.Is(err, ErrMethodNotAllowed):
		return http.StatusMethodNotAllowed, "Metode tidak diizinkan"
	case errors.Is(err, ErrResultTooLarge):
		return http.StatusUnprocessableEntity, "Hasil terlalu besar, kurangi jumlah data yang ditampilkan"
	case errors.Is(err, ErrConflict):
		return http.StatusConflict, "Data berubah, silakan muat ulang"
	case errors.Is(err, ErrRateLimited):
		return http.StatusTooManyRequests, "Terlalu banyak percobaan"
	case errors.Is(err, ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, "Layanan sementara tidak tersedia"
	default:
		return http.StatusInternalServerError, "Terjadi kesalahan"
	}
}

func errorHeaders(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, ErrRateLimited) {
		w.Header().Set("Retry-After", "60")
	}
}

func WriteHTTPError(w http.ResponseWriter, err error) {
	status, message := HTTPStatus(err)
	errorHeaders(w, err)
	http.Error(w, message, status)
}
