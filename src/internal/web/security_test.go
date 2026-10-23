package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	tests := []struct {
		name  string
		value string
	}{
		{
			name:  "Content-Security-Policy",
			value: "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'",
		},
		{
			name:  "Referrer-Policy",
			value: "strict-origin-when-cross-origin",
		},
		{
			name:  "X-Content-Type-Options",
			value: "nosniff",
		},
		{
			name:  "X-Frame-Options",
			value: "DENY",
		},
		{
			name:  "Permissions-Policy",
			value: "camera=(), microphone=(), geolocation=()",
		},
		{
			name:  "Cross-Origin-Opener-Policy",
			value: "same-origin",
		},
		{
			name:  "Cross-Origin-Resource-Policy",
			value: "same-origin",
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewRouter(logger).ServeHTTP(recorder, request)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recorder.Header().Get(tt.name)

			if got != tt.value {
				t.Errorf(
					"expected header %s to be %q, got %q",
					tt.name,
					tt.value,
					got,
				)
			}
		})
	}
}
