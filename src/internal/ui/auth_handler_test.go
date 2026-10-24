package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const testPassword = "password yang cukup panjang"

var testCookie = security.NewSecret(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))

type fakeAuth struct {
	calls    int
	input    security.Credentials
	previous security.Secret
	err      error
}

func (f *fakeAuth) Login(_ context.Context, input security.Credentials, previous security.Secret) (security.SessionResult, error) {
	f.calls++
	f.input, f.previous = input, previous
	if f.err != nil {
		return security.SessionResult{}, f.err
	}
	return security.SessionResult{CookieToken: testCookie}, nil
}

func (f *fakeAuth) Authenticate(context.Context, security.Secret, security.SessionCheck) (security.SessionResult, error) {
	return security.SessionResult{}, security.ErrUnauthenticated
}

func (f *fakeAuth) Logout(context.Context, security.Secret) error { return nil }

type fakeGuard struct{ loginGuarded int }

func (g *fakeGuard) Protected(next http.Handler) http.Handler { return next }

func (g *fakeGuard) LoginProtected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.loginGuarded++
		next.ServeHTTP(w, r)
	})
}

func (g *fakeGuard) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
}

func testAuthUI(t *testing.T, auth *fakeAuth) (*http.ServeMux, *fakeGuard) {
	t.Helper()
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := security.NewSessionCookies("http://localhost:8080", false)
	if err != nil {
		t.Fatal(err)
	}
	guard := &fakeGuard{}
	authUI, err := NewAuthUI(auth, guard, cookies, renderer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	authUI.RegisterRoutes(mux)
	return mux, guard
}

func loginRequest(body string) *http.Request {
	r := httptest.NewRequest("POST", "/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func loginForm(username, password string) string {
	return url.Values{"username": {username}, "password": {password}}.Encode()
}

func TestNewAuthUIRejectsMissingDependencies(t *testing.T) {
	renderer, _ := NewRenderer()
	cookies, _ := security.NewSessionCookies("http://localhost:8080", false)
	var nilAuth *fakeAuth
	for _, build := range []func() (*AuthUI, error){
		func() (*AuthUI, error) { return NewAuthUI(nil, &fakeGuard{}, cookies, renderer) },
		func() (*AuthUI, error) { return NewAuthUI(nilAuth, &fakeGuard{}, cookies, renderer) },
		func() (*AuthUI, error) { return NewAuthUI(&fakeAuth{}, nil, cookies, renderer) },
		func() (*AuthUI, error) { return NewAuthUI(&fakeAuth{}, &fakeGuard{}, nil, renderer) },
		func() (*AuthUI, error) { return NewAuthUI(&fakeAuth{}, &fakeGuard{}, cookies, nil) },
	} {
		if ui, err := build(); ui != nil || !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing dependency accepted")
		}
	}
}

func TestLoginPageRendersCredentialFormOnly(t *testing.T) {
	mux, _ := testAuthUI(t, &fakeAuth{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("login page not rendered safely")
	}
	if !strings.Contains(body, `name="username"`) || !strings.Contains(body, `name="password"`) || !strings.Contains(body, `action="/login"`) {
		t.Fatal("login form incomplete")
	}
	if strings.Contains(body, "csrf_token") || strings.Contains(body, "Keluar") {
		t.Fatal("anonymous login page contains session-only elements")
	}
}

func TestLoginSuccessSetsCookieAndRedirects(t *testing.T) {
	auth := &fakeAuth{}
	mux, guard := testAuthUI(t, auth)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, loginRequest(loginForm("budi", testPassword)))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/databases" || guard.loginGuarded != 1 {
		t.Fatal("successful login did not redirect through login guard")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != security.SessionCookieName || cookies[0].Value != testCookie.Reveal() || !cookies[0].HttpOnly {
		t.Fatal("session cookie not issued")
	}
	if auth.input.Username != "budi" || auth.input.Password.Reveal() != testPassword || !auth.previous.Empty() {
		t.Fatal("credentials not passed to authenticator")
	}
	if strings.Contains(w.Body.String(), testPassword) || strings.Contains(w.Body.String(), testCookie.Reveal()) {
		t.Fatal("secret leaked into response body")
	}
}

func TestLoginPassesPreviousCookieToAuthenticator(t *testing.T) {
	auth := &fakeAuth{}
	mux, _ := testAuthUI(t, auth)
	r := loginRequest(loginForm("budi", testPassword))
	r.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: testCookie.Reveal()})
	mux.ServeHTTP(httptest.NewRecorder(), r)
	if auth.previous.Reveal() != testCookie.Reveal() {
		t.Fatal("previous session cookie not handed to authenticator")
	}
}

func TestLoginFailuresRerenderFormWithoutPassword(t *testing.T) {
	for _, scenario := range []struct {
		err     error
		status  int
		message string
	}{
		{security.ErrInvalidCredentials, http.StatusUnauthorized, "Username atau password tidak valid"},
		{security.ErrRateLimited, http.StatusTooManyRequests, "Terlalu banyak percobaan"},
		{security.ErrUnavailable, http.StatusServiceUnavailable, "Layanan sementara tidak tersedia"},
		{errors.New("dial postgres://admin:canary-secret@db"), http.StatusInternalServerError, "Terjadi kesalahan"},
	} {
		mux, _ := testAuthUI(t, &fakeAuth{err: scenario.err})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, loginRequest(loginForm("budi", testPassword)))
		body := w.Body.String()
		if w.Code != scenario.status || !strings.Contains(body, scenario.message) || !strings.Contains(body, `value="budi"`) {
			t.Fatal("failed login not reported on form: " + scenario.message)
		}
		if strings.Contains(body, testPassword) || strings.Contains(body, "canary-secret") || len(w.Result().Cookies()) != 0 {
			t.Fatal("failed login leaked secret or issued cookie")
		}
		if scenario.status == http.StatusTooManyRequests && w.Header().Get("Retry-After") != "60" {
			t.Fatal("rate-limited login missing Retry-After")
		}
	}
}

func TestLoginRejectsMalformedFormBeforeAuthenticating(t *testing.T) {
	for _, body := range []string{
		"",
		"username=budi",
		loginForm("budi", testPassword) + "&csrf_token=x",
		loginForm("budi", testPassword) + "&username=other",
	} {
		auth := &fakeAuth{}
		mux, _ := testAuthUI(t, auth)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, loginRequest(body))
		if w.Code != http.StatusBadRequest || auth.calls != 0 || len(w.Result().Cookies()) != 0 {
			t.Fatal("malformed login form reached authenticator: " + body)
		}
	}
	auth := &fakeAuth{}
	mux, _ := testAuthUI(t, auth)
	r := httptest.NewRequest("POST", "/login?"+loginForm("budi", testPassword), nil)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || auth.calls != 0 {
		t.Fatal("credentials in URL accepted")
	}
}

func TestLogoutRouteUsesSecurityHandler(t *testing.T) {
	mux, _ := testAuthUI(t, &fakeAuth{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/logout", nil))
	if w.Code != http.StatusTeapot {
		t.Fatal("logout not delegated to security handler")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/logout", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("logout accepted safe method")
	}
}
