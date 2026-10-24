package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func authRequest(method, target, body string, cookie Secret) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:12345"
	if !SafeMethod(method) {
		r.Header.Set("Origin", "https://platform.example")
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if !cookie.Empty() {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie.Reveal()})
	}
	return r
}

func TestProtectedMiddlewareBlocksInvalidRequestsWithoutSideEffects(t *testing.T) {
	for _, scenario := range []string{"missing cookie", "expired", "foreign origin", "missing CSRF", "wrong CSRF", "query CSRF", "outage", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			result := f.login(t)
			r := authRequest("POST", "/edit", "csrf_token="+result.CSRFToken.Reveal(), result.CookieToken)
			want := http.StatusForbidden
			switch scenario {
			case "missing cookie":
				r.Header.Del("Cookie")
				want = http.StatusUnauthorized
			case "expired":
				f.clock.add(30 * time.Minute)
				want = http.StatusUnauthorized
			case "foreign origin":
				r.Header.Set("Origin", "https://attacker.example")
			case "missing CSRF":
				r = authRequest("POST", "/edit", "field=1", result.CookieToken)
			case "wrong CSRF":
				r = authRequest("POST", "/edit", "csrf_token="+testToken(9).Reveal(), result.CookieToken)
			case "query CSRF":
				r.URL.RawQuery = "csrf_token=" + result.CSRFToken.Reveal()
			case "outage":
				f.store.lookupErr = errors.New("private password canary")
				want = http.StatusServiceUnavailable
			case "conflict":
				f.store.advanceErr = ErrConflict
				want = http.StatusConflict
			}
			calls := 0
			handler := f.http.Protected(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != want || calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected middleware result: %d, calls=%d", w.Code, calls)
			}
			if strings.Contains(w.Body.String(), result.CookieToken.Reveal()) || strings.Contains(w.Body.String(), "private password canary") {
				t.Fatal("secret leaked in HTTP error")
			}
			if scenario != "conflict" && f.store.advanceCalls != 0 {
				t.Fatal("rejected request touched session")
			}
			if scenario == "outage" && w.Header().Get("Set-Cookie") != "" {
				t.Fatal("outage cleared a valid browser cookie")
			}
		})
	}
}

func TestProtectedMiddlewareInstallsOnlySafeViewAndPrincipal(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	handler := f.http.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok || principal != result.Principal {
			t.Error("missing principal")
		}
		identity, ok := IdentityFromContext(r.Context())
		if !ok || identity.Username != "alice" {
			t.Error("missing safe identity")
		}
		csrf, ok := CSRFTokenFromContext(r.Context())
		if !ok || csrf.Reveal() != result.CSRFToken.Reveal() {
			t.Error("missing CSRF form token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authRequest("GET", "/databases", "", result.CookieToken))
	if w.Code != http.StatusNoContent || len(w.Result().Cookies()) != 1 || w.Body.Len() != 0 {
		t.Fatal("successful protected request failed")
	}
	if _, ok := IdentityFromContext(context.Background()); ok {
		t.Fatal("anonymous context has identity")
	}
	if _, ok := CSRFTokenFromContext(context.Background()); ok {
		t.Fatal("anonymous context has CSRF")
	}
}

func TestLoginGuardChecksOriginIPAndInputBeforeHandler(t *testing.T) {
	for _, scenario := range []string{"GET", "foreign", "query", "content type", "oversized", "invalid remote", "rate limited"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			r := authRequest("POST", "/login", "username=alice&password=test", Secret{})
			want := http.StatusBadRequest
			switch scenario {
			case "GET":
				r.Method = "GET"
				want = http.StatusMethodNotAllowed
			case "foreign":
				r.Header.Set("Origin", "https://attacker.example")
				want = http.StatusForbidden
			case "query":
				r.URL.RawQuery = "password=private-canary"
			case "content type":
				r.Header.Set("Content-Type", "application/json")
			case "oversized":
				r.ContentLength = MaxLoginFormBytes + 1
			case "invalid remote":
				r.RemoteAddr = "untrusted"
			case "rate limited":
				for range 20 {
					_ = f.throttle.CheckIP(context.Background(), "192.0.2.1")
				}
				r.Header.Set("X-Forwarded-For", "203.0.113.99")
				want = http.StatusTooManyRequests
			}
			called := false
			handler := f.http.LoginProtected(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != want || called {
				t.Fatalf("login guard returned %d, handler=%v", w.Code, called)
			}
			if strings.Contains(w.Body.String(), "private-canary") {
				t.Fatal("query secret echoed")
			}
		})
	}
}

func TestLoginFormRejectsDuplicatesAndOversizedChunkedBody(t *testing.T) {
	for _, body := range []string{
		"username=alice&password=one&password=two", "username=alice", "username=alice&password=one&next=https://attacker.example",
		"username=alice&password=%zz", "username=alice&password=" + strings.Repeat("x", int(MaxLoginFormBytes)),
	} {
		r := authRequest("POST", "/login", body, Secret{})
		r.ContentLength = -1
		_, err := ReadLoginCredentials(httptest.NewRecorder(), r)
		assertAuthError(t, err, ErrInvalidInput)
	}
	input := url.Values{"username": {" ALICE "}, "password": {"  leave spaces  "}}.Encode()
	credentials, err := ReadLoginCredentials(httptest.NewRecorder(), authRequest("POST", "/login", input, Secret{}))
	if err != nil || credentials.Username != " ALICE " || credentials.Password.Reveal() != "  leave spaces  " {
		t.Fatal("form parser modified password")
	}
}

func TestLogoutHandlerRequiresCSRFForLiveSessionAndPreservesCookieOnFailure(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	w := httptest.NewRecorder()
	f.http.LogoutHandler().ServeHTTP(w, authRequest("POST", "/logout", "field=1", result.CookieToken))
	if w.Code != http.StatusForbidden || f.store.sessions[result.Principal.SessionID].RevokedAt != nil || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("unsafe logout changed session")
	}
	f.store.revokeErr = errors.New("private driver error")
	w = httptest.NewRecorder()
	f.http.LogoutHandler().ServeHTTP(w, authRequest("POST", "/logout", "csrf_token="+result.CSRFToken.Reveal(), result.CookieToken))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("failed revocation reported logout success")
	}
	f.store.revokeErr = nil
	f.clock.add(30 * time.Minute)
	w = httptest.NewRecorder()
	f.http.LogoutHandler().ServeHTTP(w, authRequest("POST", "/logout", "", result.CookieToken))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("expired logout not handled")
	}
	w = httptest.NewRecorder()
	f.http.LogoutHandler().ServeHTTP(w, authRequest("POST", "/logout", "", Secret{}))
	if w.Code != http.StatusSeeOther {
		t.Fatal("anonymous logout not idempotent")
	}
	w = httptest.NewRecorder()
	f.http.LogoutHandler().ServeHTTP(w, authRequest("GET", "/logout", "", result.CookieToken))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("GET logout allowed")
	}
}

func TestAuthHTTPErrorMappingAndConstructor(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{ErrInvalidInput, 400}, {ErrInvalidCredentials, 401}, {ErrUnauthenticated, 401}, {ErrForbidden, 403}, {ErrCSRF, 403},
		{ErrNotFound, 404}, {ErrConflict, 409}, {ErrRateLimited, 429}, {ErrUnavailable, 503}, {context.Canceled, 503}, {errors.New("secret canary"), 500},
	} {
		w := httptest.NewRecorder()
		WriteHTTPError(w, test.err)
		if w.Code != test.status || strings.Contains(w.Body.String(), "secret canary") {
			t.Fatal("unsafe or incorrect HTTP error mapping")
		}
	}
	if _, err := NewHTTPMiddleware(HTTPMiddlewareConfig{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing auth dependency accepted")
	}
}
