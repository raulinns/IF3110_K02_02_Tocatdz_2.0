package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	t.Run("returns healthy status", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		recorder := httptest.NewRecorder()

		NewRouter(logger).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf(
				"expected status %d, got %d",
				http.StatusOK,
				recorder.Code,
			)
		}

		contentType := recorder.Header().Get("Content-Type")
		if contentType != "application/json; charset=utf-8" {
			t.Errorf(
				"expected Content-Type application/json; charset=utf-8, got %q",
				contentType,
			)
		}

		if recorder.Header().Get("X-Request-ID") == "" {
			t.Error("expected X-Request-ID response header")
		}

		var response struct {
			Status string `json:"status"`
		}

		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode response body: %v", err)
		}

		if response.Status != "ok" {
			t.Errorf("expected status %q, got %q", "ok", response.Status)
		}
	})

	t.Run("rejects unsupported method", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
		recorder := httptest.NewRecorder()

		NewRouter(logger).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf(
				"expected status %d, got %d",
				http.StatusMethodNotAllowed,
				recorder.Code,
			)
		}
	})
}
