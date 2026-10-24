package security

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPStatusMatchesDefaultWriterIncludingWrappedErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{ErrInvalidInput, 400}, {ErrInvalidCredentials, 401}, {ErrUnauthenticated, 401}, {ErrForbidden, 403}, {ErrCSRF, 403},
		{ErrNotFound, 404}, {ErrMethodNotAllowed, 405}, {ErrConflict, 409}, {ErrRateLimited, 429}, {ErrResultTooLarge, 422}, {ErrUnavailable, 503},
		{context.Canceled, 503}, {context.DeadlineExceeded, 503}, {errors.Join(ErrInvalidInput, ErrPasswordTooShort), 400},
		{errors.Join(ErrInvalidInput, ErrPasswordTooLong), 400}, {errors.Join(ErrInvalidInput, ErrPasswordInvalidEncoding), 400},
		{errors.New("private-secret-canary"), 500}, {nil, 500},
	} {
		for _, err := range []error{test.err, fmt.Errorf("internal private-secret-canary: %w", test.err)} {
			status, message := HTTPStatus(err)
			if status != test.status || message == "" || strings.Contains(message, "private-secret-canary") {
				t.Fatal("unsafe status mapping")
			}
			w := httptest.NewRecorder()
			WriteHTTPError(w, err)
			if w.Code != status || strings.TrimSpace(w.Body.String()) != message || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("default writer diverged from public status mapping")
			}
			if errors.Is(err, ErrRateLimited) && w.Header().Get("Retry-After") != "60" {
				t.Fatal("rate-limit header lost")
			}
		}
	}
	_, message := HTTPStatus(ErrCSRF)
	if !strings.Contains(message, "Muat ulang halaman") {
		t.Fatal("CSRF error lacks recovery hint")
	}
}

func TestCustomErrorWriterRendersEveryGuardFailure(t *testing.T) {
	for _, scenario := range []string{"anonymous", "expired", "CSRF", "login origin", "login type", "IP limit", "login method", "logout CSRF", "logout origin", "logout outage", "logout method"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			result := f.login(t)
			cfg := f.http.deps
			calls := 0
			cfg.ErrorWriter = func(w http.ResponseWriter, r *http.Request, err error) {
				calls++
				if r.URL.Path == "" {
					t.Error("renderer did not receive original request")
				}
				status, message := HTTPStatus(err)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(status)
				_, _ = fmt.Fprintf(w, "<main><h1>%s</h1><a href='/login'>Login</a></main>", message)
			}
			middleware, err := NewHTTPMiddleware(cfg)
			if err != nil {
				t.Fatal(err)
			}
			invoked := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invoked = true })
			var handler http.Handler = middleware.Protected(next)
			r := authRequest("POST", "/edit", "csrf_token="+result.CSRFToken.Reveal(), result.CookieToken)
			want := http.StatusForbidden
			switch scenario {
			case "anonymous":
				r = authRequest("GET", "/databases", "", Secret{})
				want = 401
			case "expired":
				r = authRequest("GET", "/databases", "", result.CookieToken)
				f.clock.add(DefaultSessionPolicy().IdleTimeout)
				want = 401
			case "CSRF":
				r = authRequest("POST", "/edit", "csrf_token="+testToken(9).Reveal(), result.CookieToken)
			case "login origin":
				handler = middleware.LoginProtected(next)
				r = authRequest("POST", "/login", "username=alice&password=test", Secret{})
				r.Header.Set("Origin", "https://attacker.example")
			case "login type":
				handler = middleware.LoginProtected(next)
				r = authRequest("POST", "/login", "{}", Secret{})
				r.Header.Set("Content-Type", "application/json")
				want = 400
			case "IP limit":
				handler = middleware.LoginProtected(next)
				r = authRequest("POST", "/login", "username=alice&password=test", Secret{})
				want = 429
				for range 20 {
					_ = f.throttle.CheckIP(context.Background(), "192.0.2.1")
				}
			case "login method":
				handler = middleware.LoginProtected(next)
				r = authRequest("GET", "/login", "", Secret{})
				want = 405
			case "logout CSRF":
				handler = middleware.LogoutHandler()
				r = authRequest("POST", "/logout", "field=1", result.CookieToken)
			case "logout origin":
				handler = middleware.LogoutHandler()
				r = authRequest("POST", "/logout", "", result.CookieToken)
				r.Header.Set("Origin", "https://attacker.example")
			case "logout outage":
				handler = middleware.LogoutHandler()
				r = authRequest("POST", "/logout", "csrf_token="+result.CSRFToken.Reveal(), result.CookieToken)
				f.store.revokeErr = errors.New("private-secret-canary")
				want = 503
			case "logout method":
				handler = middleware.LogoutHandler()
				r = authRequest("GET", "/logout", "", result.CookieToken)
				want = 405
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != want || calls != 1 || invoked || !strings.HasPrefix(w.Body.String(), "<main>") {
				t.Fatalf("unexpected custom rendering: code=%d calls=%d next=%v", w.Code, calls, invoked)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("custom renderer lost cache protection")
			}
			if want == 429 && w.Header().Get("Retry-After") != "60" {
				t.Fatal("custom renderer lost rate-limit header")
			}
			if want == 405 && w.Header().Get("Allow") != "POST" {
				t.Fatal("custom renderer lost Allow header")
			}
			if want == 401 && (len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1) {
				t.Fatal("invalid cookie was not cleared before rendering")
			}
			if want != 401 && w.Header().Get("Set-Cookie") != "" {
				t.Fatal("guard failure changed browser session")
			}
			if f.store.advanceCalls != 0 || f.store.sessions[result.Principal.SessionID].RevokedAt != nil {
				t.Fatal("custom renderer bypassed security checks")
			}
			if strings.Contains(w.Body.String(), result.CookieToken.Reveal()) || strings.Contains(w.Body.String(), "private-secret-canary") {
				t.Fatal("custom error response leaked a secret")
			}
		})
	}
}

func TestErrorRendererCannotAccidentallyTurnFailureIntoSuccess(t *testing.T) {
	f := newAuthFixture(t)
	cfg := f.http.deps
	cfg.ErrorWriter = func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Del("Cache-Control")
		w.Header().Del("Retry-After")
		w.Header().Del("Allow")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<main>Error</main>"))
	}
	middleware, err := NewHTTPMiddleware(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	middleware.LoginProtected(nil).ServeHTTP(w, authRequest("GET", "/login", "", Secret{}))
	if w.Code != 405 || w.Header().Get("Allow") != "POST" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("renderer overrode guard status/headers")
	}
	for range 20 {
		_ = f.throttle.CheckIP(context.Background(), "192.0.2.1")
	}
	w = httptest.NewRecorder()
	middleware.LoginProtected(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("rate limited handler ran") })).ServeHTTP(w, authRequest("POST", "/login", "username=alice&password=test", Secret{}))
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("renderer overrode rate limit status/headers")
	}
	cfg.ErrorWriter = func(http.ResponseWriter, *http.Request, error) {}
	middleware, _ = NewHTTPMiddleware(cfg)
	w = httptest.NewRecorder()
	middleware.Protected(nil).ServeHTTP(w, authRequest("GET", "/databases", "", Secret{}))
	if w.Code != 503 {
		t.Fatal("empty renderer implicitly returned 200")
	}
}

func TestMiddlewareInterfaceExposesLogoutWithoutConcreteType(t *testing.T) {
	f := newAuthFixture(t)
	var guards Middleware = f.http
	w := httptest.NewRecorder()
	guards.LogoutHandler().ServeHTTP(w, authRequest("POST", "/logout", "", Secret{}))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatal("logout is not usable through shared interface")
	}
}
