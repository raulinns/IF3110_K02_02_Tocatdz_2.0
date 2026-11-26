package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
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
