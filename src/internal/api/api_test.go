package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
)

func TestAPIRoutingErrorsHeadersAndRequestCorrelation(t *testing.T) {
	f := newFixture(t)
	a := buildAPI(t, f, Route{Method: "GET", Path: "/objects/{object_id}", Access: Public, QueryParameters: []string{"page"}, Handle: func(w http.ResponseWriter, r *http.Request) error {
		if r.PathValue("object_id") == "invalid" {
			return &ValidationError{Fields: map[string][]string{"id": {"Invalid object"}}}
		}
		return WriteJSON(w, 200, map[string]string{"id": r.PathValue("object_id")})
	}})
	for _, test := range []struct {
		method, path string
		status       int
		code         string
	}{
		{"GET", Prefix + "/objects/abc", 200, ""},
		{"HEAD", Prefix + "/objects/abc", 200, ""},
		{"GET", Prefix + "/objects/invalid", 422, "validation_failed"},
		{"GET", Prefix + "/objects/abc?extra=1", 400, "bad_request"},
		{"GET", Prefix + "/objects/abc?page=1&page=2", 400, "bad_request"},
		{"GET", Prefix + "/unknown", 404, "not_found"},
		{"GET", Prefix, 404, "not_found"},
		{"GET", "/elsewhere", 404, "not_found"},
		{"POST", Prefix + "/objects/abc", 405, "method_not_allowed"},
		{"OPTIONS", Prefix + "/objects/abc", 405, "method_not_allowed"},
		{"GET", Prefix + "//objects/abc", 400, "bad_request"},
	} {
		r := request(test.method, test.path, "", security.Secret{}, security.Secret{})
		r.Header.Set("X-Request-ID", "untrusted-canary")
		w := dispatch(a, r)
		if w.Code != test.status {
			t.Fatalf("%s %s: %d %s", test.method, test.path, w.Code, w.Body.String())
		}
		if len(w.Header().Get("X-Request-ID")) != 32 || w.Header().Get("X-Request-ID") == "untrusted-canary" {
			t.Fatal("request ID not server-generated")
		}
		for header, want := range map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "strict-origin-when-cross-origin", "Content-Security-Policy": security.ContentSecurityPolicy} {
			if w.Header().Get(header) != want {
				t.Errorf("%s missing", header)
			}
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("API enabled CORS")
		}
		if test.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("wrong Allow: %s", w.Header().Get("Allow"))
		}
		if test.method == "HEAD" {
			if w.Body.Len() != 0 {
				t.Fatal("HEAD wrote a body")
			}
			continue
		}
		if test.code != "" {
			var body ErrorEnvelope
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.Code != test.code || body.Error.RequestID != w.Header().Get("X-Request-ID") {
				t.Fatal("error and header correlation differ")
			}
		}
	}
	// API and existing HTML middleware must agree on one trusted correlation ID.
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)
	// Use the existing registrar hook without changing integrator-owned router.go.
	outer := web.NewRouter(f.cfg.Logger, a)
	w := dispatch(outer, request("GET", Prefix+"/objects/invalid", "", security.Secret{}, security.Secret{}))
	var body ErrorEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatal("mounted API lost request ID")
	}
	ctx := security.WithRequestID(context.Background(), "trusted-outer-id")
	r := request("GET", Prefix+"/unknown", "", security.Secret{}, security.Secret{}).WithContext(ctx)
	w = dispatch(mux, r)
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Header().Get("X-Request-ID") != "trusted-outer-id" || body.Error.RequestID != "trusted-outer-id" {
		t.Fatal("trusted outer request context replaced")
	}
}

func TestAPIRecoversBeforeCommitAndAbortsAfterCommitWithoutLeaks(t *testing.T) {
	f := newFixture(t)
	for _, afterCommit := range []bool{false, true} {
		a := buildAPI(t, f, Route{Method: "GET", Path: "/panic", Access: Public, Handle: func(w http.ResponseWriter, r *http.Request) error {
			if afterCommit {
				_, _ = w.Write([]byte("partial"))
			}
			panic("password-panic-canary")
		}})
		w := httptest.NewRecorder()
		func() {
			defer func() {
				failure := recover()
				if afterCommit && failure != http.ErrAbortHandler {
					t.Errorf("must abort started response, got %v", failure)
				}
				if !afterCommit && failure != nil {
					t.Errorf("unexpected escape: %v", failure)
				}
			}()
			a.ServeHTTP(w, request("GET", Prefix+"/panic", "", security.Secret{}, security.Secret{}))
		}()
		if afterCommit {
			if w.Body.String() != "partial" {
				t.Fatal("appended error after response started")
			}
		} else if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"internal"`) {
			t.Fatal("panic response not JSON 500")
		}
		if strings.Contains(w.Body.String(), "canary") || strings.Contains(f.logs.String(), "canary") {
			t.Fatal("panic details leaked")
		}
	}
	a := buildAPI(t, f, Route{Method: "GET", Path: "/encoding", Access: Public, Handle: func(w http.ResponseWriter, _ *http.Request) error {
		return WriteJSON(w, 200, security.NewSecret("canary"))
	}})
	w := dispatch(a, request("GET", Prefix+"/encoding", "", security.Secret{}, security.Secret{}))
	if w.Code != 500 || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("unencodable response did not fail atomically")
	}
}

func TestAPIConstructorRejectsUnsafeOrConflictingRouteConfiguration(t *testing.T) {
	f := newFixture(t)
	endpoint := func(http.ResponseWriter, *http.Request) error { return nil }
	for _, route := range []Route{
		{Method: "GET", Path: "/x"},
		{Method: "GET", Path: "/x", Access: Access(99), Handle: endpoint},
		{Method: "POST", Path: "/x", Access: Public, Handle: endpoint},
		{Method: "GET", Path: "/x", Body: JSONBody, Handle: endpoint},
		{Method: "GET", Path: "/bad?query", Handle: endpoint},
		{Method: "get", Path: "/x", Handle: endpoint},
		{Method: "GET", Path: "/bad/{", Handle: endpoint},
	} {
		cfg := f.cfg
		cfg.Routes = []Route{route}
		if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatalf("invalid route accepted: %+v", route)
		}
	}
	cfg := f.cfg
	cfg.Routes = []Route{{Method: "GET", Path: "/x", Handle: endpoint}, {Method: "GET", Path: "/x", Handle: endpoint}}
	if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("duplicate route accepted")
	}
	cfg = f.cfg
	cfg.Logger = nil
	if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("missing logger accepted")
	}
	cfg = f.cfg
	cfg.Security.Auth = nil
	if _, err := New(cfg); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("missing auth accepted")
	}
}

func TestRouteBodyPoliciesRejectInvalidInputBeforeEndpoint(t *testing.T) {
	f := newFixture(t)
	session := fixtureLogin(t, f)
	calls := 0
	a := buildAPI(t, f,
		Route{Method: "POST", Path: "/json", Body: JSONBody, Handle: func(w http.ResponseWriter, r *http.Request) error { calls++; w.WriteHeader(204); return nil }},
		Route{Method: "DELETE", Path: "/empty", Handle: func(w http.ResponseWriter, r *http.Request) error { calls++; w.WriteHeader(204); return nil }},
	)
	for _, scenario := range []string{"nil body", "wrong type", "too large", "unexpected body"} {
		r := request("POST", Prefix+"/json", `{}`, session.CookieToken, session.CSRFToken)
		want := 400
		switch scenario {
		case "nil body":
			r.Body = nil
		case "wrong type":
			r.Header.Set("Content-Type", "text/plain")
			want = 415
		case "too large":
			r.ContentLength = MaxJSONBodyBytes + 1
			want = 413
		case "unexpected body":
			r.Method = "DELETE"
			r.URL.Path = Prefix + "/empty"
		}
		w := dispatch(a, r)
		if w.Code != want || calls != 0 {
			t.Fatalf("%s: status=%d calls=%d", scenario, w.Code, calls)
		}
	}
}

func TestAPIIsProtectedByDefaultAndPreservesJSONStatusFromGuard(t *testing.T) {
	f := newFixture(t)
	calls := 0
	a := buildAPI(t, f, Route{Method: "GET", Path: "/private", Handle: func(http.ResponseWriter, *http.Request) error { calls++; return nil }})
	w := dispatch(a, request("GET", Prefix+"/private", "", security.Secret{}, security.Secret{}))
	if w.Code != 401 || calls != 0 || !strings.Contains(w.Body.String(), `"code":"unauthenticated"`) {
		t.Fatal("default route was not authenticated")
	}
	// A domain error originating inside the guard must retain the documented 422 rather
	// than being forced back to the HTML mapper's 400.
	session := fixtureLogin(t, f)
	f.cfg.Security.Auth = authWithDomainFailure{Authenticator: f.auth}
	a = buildAPI(t, f, Route{Method: "GET", Path: "/private", Handle: func(http.ResponseWriter, *http.Request) error { calls++; return nil }})
	w = dispatch(a, request("GET", Prefix+"/private", "", session.CookieToken, security.Secret{}))
	if w.Code != 422 || calls != 0 || !strings.Contains(w.Body.String(), `"code":"validation_failed"`) {
		t.Fatal("HTML status mapper overrode API guard response")
	}
}

type authWithDomainFailure struct{ security.Authenticator }

func (authWithDomainFailure) Authenticate(context.Context, security.Secret, security.SessionCheck) (security.SessionResult, error) {
	return security.SessionResult{}, security.ErrInvalidInput
}
