package security

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func jsonTestConfig(f *authFixture) HTTPMiddlewareConfig {
	cfg := f.http.deps
	cfg.Transport = JSONTransport
	cfg.ErrorStatus = func(err error) int {
		if errors.Is(err, ErrInvalidInput) {
			return 422
		}
		status, _ := HTTPStatus(err)
		return status
	}
	cfg.ErrorWriter = func(w http.ResponseWriter, _ *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(cfg.ErrorStatus(err))
		_, _ = io.WriteString(w, `{"error":"guard"}`)
	}
	return cfg
}

func TestJSONLogoutUsesHeaderTokenAndReturnsNoContent(t *testing.T) {
	f := newAuthFixture(t)
	session := f.login(t)
	m, err := NewHTTPMiddleware(jsonTestConfig(f))
	if err != nil {
		t.Fatal(err)
	}
	r := authRequest("POST", "/api/v1/auth/logout", "", session.CookieToken)
	r.Header.Set(CSRFHeaderName, session.CSRFToken.Reveal())
	w := httptest.NewRecorder()
	m.LogoutHandler().ServeHTTP(w, r)
	if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("JSON logout: %d %s", w.Code, w.Body.String())
	}
	if f.store.sessions[session.Principal.SessionID].RevokedAt == nil {
		t.Fatal("logout did not revoke session")
	}
	w = httptest.NewRecorder()
	m.Protected(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("revoked session accepted") })).ServeHTTP(w, authRequest("GET", "/private", "", session.CookieToken))
	if w.Code != 401 {
		t.Fatal("revoked session not rejected")
	}
	r = authRequest("POST", "/api/v1/auth/logout", "", session.CookieToken)
	r.Header.Set(CSRFHeaderName, session.CSRFToken.Reveal())
	w = httptest.NewRecorder()
	m.LogoutHandler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("JSON logout pretended revoked session was active")
	}
}

func TestJSONLogoutRejectsInvalidRequestsBeforeRevocation(t *testing.T) {
	for _, scenario := range []string{"body", "missing header", "wrong header", "duplicate header", "query token", "foreign origin", "anonymous"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			session := f.login(t)
			m, err := NewHTTPMiddleware(jsonTestConfig(f))
			if err != nil {
				t.Fatal(err)
			}
			r := authRequest("POST", "/api/v1/auth/logout", "", session.CookieToken)
			r.Header.Set(CSRFHeaderName, session.CSRFToken.Reveal())
			want := 403
			switch scenario {
			case "body":
				r.Body = io.NopCloser(strings.NewReader(`{}`))
				want = 400
			case "missing header":
				r.Header.Del(CSRFHeaderName)
			case "wrong header":
				r.Header.Set(CSRFHeaderName, testToken(9).Reveal())
			case "duplicate header":
				r.Header.Add(CSRFHeaderName, session.CSRFToken.Reveal())
			case "query token":
				r.URL.RawQuery = "csrf_token=" + session.CSRFToken.Reveal()
			case "foreign origin":
				r.Header.Set("Origin", "https://attacker.example")
			case "anonymous":
				r.Header.Del("Cookie")
				want = 401
			}
			w := httptest.NewRecorder()
			m.LogoutHandler().ServeHTTP(w, r)
			if w.Code != want || f.store.sessions[session.Principal.SessionID].RevokedAt != nil {
				t.Fatalf("%s: status=%d", scenario, w.Code)
			}
		})
	}
}

func TestJSONLoginGuardValidatesTransportWithoutConsumingBody(t *testing.T) {
	for _, scenario := range []string{"valid", "media", "charset", "query", "large", "remote", "origin"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			m, err := NewHTTPMiddleware(jsonTestConfig(f))
			if err != nil {
				t.Fatal(err)
			}
			r := authRequest("POST", "/api/v1/auth/login", `{"username":"alice"}`, Secret{})
			r.Header.Set("Content-Type", "application/json")
			want := 204
			switch scenario {
			case "media":
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				want = 415
			case "charset":
				r.Header.Set("Content-Type", "application/json; charset=latin1")
				want = 415
			case "query":
				r.URL.RawQuery = "password=canary"
				want = 400
			case "large":
				r.ContentLength = MaxLoginFormBytes + 1
				want = 413
			case "remote":
				r.RemoteAddr = "invalid"
				want = 400
			case "origin":
				r.Header.Set("Origin", "https://attacker.example")
				want = 403
			}
			calls := 0
			w := httptest.NewRecorder()
			m.LoginProtected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != `{"username":"alice"}` {
					t.Error("login body consumed by guard")
				}
				w.WriteHeader(204)
			})).ServeHTTP(w, r)
			if w.Code != want || (want != 204 && calls != 0) {
				t.Fatalf("%s: %d calls=%d", scenario, w.Code, calls)
			}
		})
	}
}

type authWithFailure struct {
	Authenticator
	failure error
}

func (a authWithFailure) Authenticate(context.Context, Secret, SessionCheck) (SessionResult, error) {
	return SessionResult{}, a.failure
}

func TestCustomStatusMapperCannotTurnSecurityFailureIntoSuccess(t *testing.T) {
	f := newAuthFixture(t)
	session := f.login(t)
	for _, status := range []int{422, 200, 700} {
		cfg := jsonTestConfig(f)
		cfg.Auth = authWithFailure{Authenticator: f.service, failure: ErrInvalidInput}
		cfg.ErrorStatus = func(error) int { return status }
		// The renderer must not override the separately selected guard status.
		cfg.ErrorWriter = func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.Header().Del("Cache-Control")
			w.WriteHeader(200)
		}
		m, err := NewHTTPMiddleware(cfg)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		m.Protected(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("failed auth invoked endpoint") })).ServeHTTP(w, authRequest("GET", "/private", "", session.CookieToken))
		want := status
		if want < 400 || want > 599 {
			want = 500
		}
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status mapper=%d result=%d", status, w.Code)
		}
	}
	cfg := f.http.deps
	cfg.Transport = JSONTransport
	cfg.ErrorStatus = nil
	cfg.ErrorWriter = nil
	if _, err := NewHTTPMiddleware(cfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("JSON mode accepted missing status/writer adapter")
	}
}
