package web

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareStack(t *testing.T) {
	t.Run("adds request ID and logs request result", func(t *testing.T) {
		var logs bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&logs, nil))

		handler := middlewareStack(
			logger,
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}),
		)

		request := httptest.NewRequest(http.MethodGet, "/example?secret=value", nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		requestID := recorder.Header().Get("X-Request-ID")
		if requestID == "" {
			t.Fatal("expected X-Request-ID response header")
		}

		decodedID, err := hex.DecodeString(requestID)
		if err != nil {
			t.Fatalf("expected hexadecimal request ID, got %q", requestID)
		}

		if len(decodedID) != 16 {
			t.Errorf(
				"expected 16 random bytes, got %d",
				len(decodedID),
			)
		}

		logOutput := logs.String()

		expectedFragments := []string{
			`"msg":"HTTP request"`,
			`"request_id":"` + requestID + `"`,
			`"method":"GET"`,
			`"route":"[unmatched]"`,
			`"status":204`,
		}

		for _, fragment := range expectedFragments {
			if !strings.Contains(logOutput, fragment) {
				t.Errorf(
					"expected log to contain %q, got %q",
					fragment,
					logOutput,
				)
			}
		}

		if strings.Contains(logOutput, "secret=value") {
			t.Errorf(
				"query string must not appear in log: %q",
				logOutput,
			)
		}
	})

	t.Run("recovers from panic without exposing panic value", func(t *testing.T) {
		var logs bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&logs, nil))

		handler := middlewareStack(
			logger,
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("sensitive panic value")
			}),
		)

		request := httptest.NewRequest(http.MethodGet, "/panic", nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf(
				"expected status %d, got %d",
				http.StatusInternalServerError,
				recorder.Code,
			)
		}

		if recorder.Header().Get("X-Request-ID") == "" {
			t.Error("expected X-Request-ID response header")
		}

		if !strings.Contains(logs.String(), `"msg":"panic recovered"`) {
			t.Errorf("expected recovery log, got %q", logs.String())
		}

		if strings.Contains(logs.String(), "sensitive panic value") {
			t.Errorf(
				"panic value must not appear in log: %q",
				logs.String(),
			)
		}
	})
}
