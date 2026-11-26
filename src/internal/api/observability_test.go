package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging/sqllog"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestAPICompletionLogUsesTemplateAndTrustedActor(t *testing.T) {
	f := newFixture(t)
	f.cfg.Logger = logging.New(&f.logs)
	api := buildAPI(t, f, Route{Method: http.MethodGet, Path: "/databases/{database_id}/sample", Handle: func(w http.ResponseWriter, _ *http.Request) error {
		return WriteJSON(w, http.StatusOK, struct{}{})
	}})
	path := Prefix + "/databases/" + string(testDatabase) + "/sample"
	denied := request(http.MethodGet, path+"?unexpected=private-query", "", security.Secret{}, security.Secret{})
	denied.Header.Set("X-Request-ID", "private-client-id")
	w := dispatch(api, denied)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected auth failure, got %d", w.Code)
	}
	session := fixtureLogin(t, f)
	w = dispatch(api, request(http.MethodGet, path, "", session.CookieToken, security.Secret{}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected authorized request, got %d", w.Code)
	}
	logs := f.logs.String()
	for _, fragment := range []string{
		`"event":"http.request"`, `"http_route":"/api/v1/databases/{database_id}/sample"`,
		`"actor":"anonymous"`, `"http_status":401`, `"actor":"` + string(testUser) + `"`,
		`"request_id":"` + w.Header().Get("X-Request-ID") + `"`,
	} {
		if !strings.Contains(logs, fragment) {
			t.Errorf("missing completion field %s", fragment)
		}
	}
	if strings.Contains(logs, "private-") || strings.Contains(logs, session.CookieToken.Reveal()) {
		t.Fatal("completion log contained client-controlled request data")
	}
}

func TestAPITelemetryEvidence(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var spans bytes.Buffer
	shutdown, err := tracing.StartProcess(context.Background(), "tocatdz-api-test", &spans)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown() })
	f := newFixture(t)
	f.cfg.Logger = logging.New(&f.logs)
	f.cfg.Auth = &AuthConfig{Leaders: &fakeLeadership{}}
	a := buildAPI(t, f, Route{Method: http.MethodGet, Path: "/databases/{database_id}/sample", Handle: func(w http.ResponseWriter, r *http.Request) error {
		ctx := sqllog.Start(r.Context(), sqllog.Target, security.NewSecret("SELECT 'literal-canary' WHERE id = $1"), 1)
		sqllog.Done(ctx, int64(1), nil)
		return WriteJSON(w, http.StatusOK, struct{}{})
	}})
	for _, test := range []struct {
		password string
		status   int
	}{
		{"password-canary", http.StatusUnauthorized},
		{"correct test password", http.StatusOK},
	} {
		body, _ := json.Marshal(map[string]string{"username": "viewer", "password": test.password})
		w := dispatch(a, request(http.MethodPost, Prefix+"/auth/login", string(body), security.Secret{}, security.Secret{}))
		if w.Code != test.status {
			t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
		}
	}
	session := fixtureLogin(t, f)
	r := request(http.MethodGet, Prefix+"/databases/"+string(testDatabase)+"/sample", "", session.CookieToken, security.Secret{})
	r.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if w := dispatch(a, r); w.Code != http.StatusOK {
		t.Fatalf("query status=%d", w.Code)
	}
	if err := shutdown(); err != nil {
		t.Fatal(err)
	}
	spanByID := map[string]map[string]any{}
	for _, line := range bytes.Split(bytes.TrimSpace(spans.Bytes()), []byte("\n")) {
		var span map[string]any
		if err := json.Unmarshal(line, &span); err != nil {
			t.Fatal(err)
		}
		spanByID[span["span_id"].(string)] = span
		if span["trace_id"] == "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatal("API trusted a browser-supplied parent")
		}
		t.Logf("K5_SPAN %s", line)
	}
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(f.logs.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		span := spanByID[record["span_id"].(string)]
		if span == nil || span["trace_id"] != record["trace_id"] {
			t.Fatal("API log has no matching exported span")
		}
		seen[record["event"].(string)] = true
		if record["event"] == "sql.query" && (record["actor"] != string(testUser) || record["database_id"] != string(testDatabase)) {
			t.Fatal("read query lost authenticated actor/database")
		}
		t.Logf("K5_LOG %s", line)
	}
	if !seen["auth.login"] || !seen["sql.query"] || !seen["http.request"] {
		t.Fatal("missing login, read SQL or HTTP completion evidence")
	}
	if strings.Contains(f.logs.String()+spans.String(), "canary") || strings.Contains(f.logs.String(), session.CookieToken.Reveal()) {
		t.Fatal("sensitive input escaped to telemetry")
	}
}
