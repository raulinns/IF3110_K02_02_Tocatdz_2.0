package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogsExcludeClientControlledURLAndPanicData(t *testing.T) {
	for _, target := range []string{"/items/URL_PRIVATE_CANARY", "/unmatched/URL_PRIVATE_CANARY", "/panic/URL_PRIVATE_CANARY"} {
		var logs bytes.Buffer
		mux := http.NewServeMux()
		mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		mux.HandleFunc("GET /panic/{id}", func(http.ResponseWriter, *http.Request) { panic("PANIC_PRIVATE_CANARY") })
		handler := middlewareStack(slog.New(slog.NewJSONHandler(&logs, nil)), mux)
		r := httptest.NewRequest("GET", target+"?password=QUERY_PRIVATE_CANARY", strings.NewReader("BODY_PRIVATE_CANARY"))
		r.Header.Set("Cookie", "tocatdz_session=COOKIE_PRIVATE_CANARY")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		for _, canary := range []string{"URL_PRIVATE_CANARY", "QUERY_PRIVATE_CANARY", "BODY_PRIVATE_CANARY", "COOKIE_PRIVATE_CANARY", "PANIC_PRIVATE_CANARY"} {
			if strings.Contains(logs.String(), canary) {
				t.Fatal("request log leaked client data")
			}
		}
		if strings.Contains(target, "/items/") && !strings.Contains(logs.String(), `"route":"GET /items/{id}"`) {
			t.Fatal("matched route template missing")
		}
		if strings.Contains(target, "/panic/") && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("panic response may be cached")
		}
	}
}

func TestUnknownHTTPMethodDoesNotEnterLogs(t *testing.T) {
	var logs bytes.Buffer
	handler := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("METHOD_PRIVATE_CANARY", "/healthz", nil))
	if strings.Contains(logs.String(), "METHOD_PRIVATE_CANARY") || !strings.Contains(logs.String(), `"method":"[other]"`) {
		t.Fatal("unknown method was logged verbatim")
	}
}
