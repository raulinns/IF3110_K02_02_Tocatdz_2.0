package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestErrorResponseMatchesSecurityMapping(t *testing.T) {
	for _, err := range []error{
		security.ErrInvalidInput, security.ErrInvalidCredentials, security.ErrUnauthenticated,
		security.ErrForbidden, security.ErrCSRF, security.ErrNotFound, security.ErrConflict,
		security.ErrRateLimited, security.ErrUnavailable, context.Canceled, context.DeadlineExceeded,
		security.ErrResultTooLarge, security.ErrMethodNotAllowed, security.ErrPasswordTooShort,
		security.ErrPasswordTooLong, security.ErrPasswordInvalidEncoding,
		fmt.Errorf("load database: %w", security.ErrForbidden), errors.New("unexpected"),
	} {
		w := httptest.NewRecorder()
		security.WriteHTTPError(w, err)
		if status, message := errorResponse(err); status != w.Code || message+"\n" != w.Body.String() {
			t.Fatal("ui error mapping drifted from security for: " + err.Error())
		}
	}
}

func TestRenderErrorPage(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := r.RenderError(w, httptest.NewRequest("GET", "/", nil), security.ErrForbidden); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Akses ditolak") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("forbidden page rendered incorrectly")
	}
	w = httptest.NewRecorder()
	_ = r.RenderError(w, httptest.NewRequest("GET", "/", nil), security.ErrRateLimited)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatal("rate limit page missing Retry-After")
	}
	w = httptest.NewRecorder()
	_ = r.RenderError(w, httptest.NewRequest("GET", "/", nil), errors.New("dial postgres://admin:canary-secret@db"))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "canary-secret") {
		t.Fatal("unexpected error leaked internal details")
	}
}
