package ui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestRenderErrorFollowsSecurityMapping(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		security.ErrInvalidInput, security.ErrInvalidCredentials, security.ErrUnauthenticated,
		security.ErrForbidden, security.ErrCSRF, security.ErrNotFound, security.ErrConflict,
		security.ErrRateLimited, security.ErrUnavailable, security.ErrMethodNotAllowed, context.Canceled,
		security.ValidateNewPassword(security.NewSecret("pendek")),
		fmt.Errorf("load database: %w", security.ErrForbidden), errors.New("unexpected"),
	} {
		w := httptest.NewRecorder()
		_ = r.RenderError(w, httptest.NewRequest("GET", "/", nil), err)
		status, message := security.HTTPStatus(err)
		if w.Code != status || !strings.Contains(w.Body.String(), html.EscapeString(message)) {
			t.Fatal("error page does not follow security mapping for: " + err.Error())
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
