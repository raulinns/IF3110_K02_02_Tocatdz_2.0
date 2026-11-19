package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestErrorEnvelopeMatchesK1AndNeverEchoesInternalErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{security.ErrMalformedRequest, 400, "bad_request"}, {security.ErrPayloadTooLarge, 413, "payload_too_large"},
		{security.ErrUnsupportedMediaType, 415, "unsupported_media_type"}, {security.ErrInvalidCredentials, 401, "unauthenticated"},
		{security.ErrUnauthenticated, 401, "unauthenticated"}, {security.ErrForbidden, 403, "forbidden"},
		{security.ErrCSRF, 403, "csrf_failed"}, {security.ErrNotFound, 404, "not_found"},
		{security.ErrMethodNotAllowed, 405, "method_not_allowed"}, {security.ErrConflict, 409, "conflict"},
		{security.ErrInvalidInput, 422, "validation_failed"}, {security.ErrPasswordTooShort, 422, "validation_failed"},
		{security.ErrTargetNotReady, 422, "validation_failed"}, {security.ErrResultTooLarge, 422, "result_too_large"},
		{security.ErrRateLimited, 429, "rate_limited"}, {security.ErrUnavailable, 503, "unavailable"},
		{context.Canceled, 503, "unavailable"}, {context.DeadlineExceeded, 503, "unavailable"},
		{errors.New("private-password-canary"), 500, "internal"},
	} {
		err := fmt.Errorf("private-dsn-canary: %w", test.err)
		r := httptest.NewRequest("GET", "/", nil)
		r = r.WithContext(security.WithRequestID(r.Context(), "server-id"))
		w := httptest.NewRecorder()
		WriteError(w, r, err)
		var envelope ErrorEnvelope
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || w.Code != test.status || envelope.Error.Code != test.code || envelope.Error.RequestID != "server-id" {
			t.Fatalf("error mapping %v: %d %s", test.err, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "canary") || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatal("unsafe error output")
		}
		if test.status == 429 && w.Header().Get("Retry-After") != "60" {
			t.Fatal("missing Retry-After")
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	WriteError(w, r, &ValidationError{Fields: map[string][]string{"down_sql": {"SQL down wajib diisi"}}})
	var envelope ErrorEnvelope
	if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || w.Code != 422 || len(envelope.Error.Fields["down_sql"]) != 1 {
		t.Fatal("field validation errors lost")
	}
}

func TestQueryAndPathIDValidation(t *testing.T) {
	for _, query := range []string{"extra=1", "page=1&page=2", "%xx=1", "page=1;extra=2"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.RawQuery = query
		if _, err := ParseQuery(r, "page"); !errors.Is(err, security.ErrMalformedRequest) {
			t.Errorf("query accepted: %s", query)
		}
	}
	r := httptest.NewRequest("GET", "/?page=1", nil)
	if q, err := ParseQuery(r, "page"); err != nil || q.Get("page") != "1" {
		t.Fatal("valid query rejected")
	}
	for _, id := range []string{"", "bad", "00000000-0000-0000-0000-000000000000", "ABCDEF00-0000-4000-8000-000000000001"} {
		r.SetPathValue("database_id", id)
		if _, err := PathID(r, "database_id"); !errors.Is(err, security.ErrMalformedRequest) {
			t.Errorf("invalid UUID accepted: %s", id)
		}
	}
	r.SetPathValue("database_id", "10000000-0000-4000-8000-000000000001")
	if _, err := PathID(r, "database_id"); err != nil {
		t.Fatal(err)
	}
}
