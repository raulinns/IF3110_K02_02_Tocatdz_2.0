package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestExpiredPreviousCookieCannotRevokeLiveSession(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	f.clock.add(f.service.deps.Policy.RotationInterval)
	current, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.add(f.service.deps.Policy.GracePeriod)
	checked := false
	err = f.service.LogoutWithCheck(context.Background(), first.CookieToken, func(context.Context, Principal) error { checked = true; return ErrCSRF })
	if err != nil || checked {
		t.Fatal("expired cookie should behave as an anonymous logout")
	}
	if _, err := f.service.Authenticate(context.Background(), current.CookieToken, nil); err != nil {
		t.Fatal("expired previous cookie revoked the current session")
	}
}

func TestPreviousCookieWithinGraceStillRequiresCSRF(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	f.clock.add(f.service.deps.Policy.RotationInterval)
	current, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.add(time.Second)
	assertAuthError(t, f.service.LogoutWithCheck(context.Background(), first.CookieToken, func(context.Context, Principal) error { return ErrCSRF }), ErrCSRF)
	if _, err := f.service.Authenticate(context.Background(), current.CookieToken, nil); err != nil {
		t.Fatal("rejected logout revoked the session")
	}
}

func TestHTTPLogoutPreviousCookieBoundariesIntegration(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		offset  time.Duration
		csrf    bool
		status  int
		revoked bool
	}{
		{"live missing CSRF", -time.Nanosecond, false, http.StatusForbidden, false},
		{"live valid CSRF", -time.Nanosecond, true, http.StatusSeeOther, true},
		{"grace equality", 0, false, http.StatusSeeOther, false},
		{"after grace", time.Nanosecond, false, http.StatusSeeOther, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newAuthFixture(t)
			first := f.login(t)
			f.clock.add(f.service.deps.Policy.RotationInterval)
			current, err := f.service.Authenticate(t.Context(), first.CookieToken, nil)
			if err != nil {
				t.Fatal(err)
			}
			f.clock.add(f.service.deps.Policy.GracePeriod + scenario.offset)
			body := ""
			if scenario.csrf {
				body = url.Values{CSRFFieldName: {current.CSRFToken.Reveal()}}.Encode()
			}
			w := httptest.NewRecorder()
			f.http.LogoutHandler().ServeHTTP(w, authRequest("POST", "/logout", body, first.CookieToken))
			if w.Code != scenario.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("incorrect logout boundary response")
			}
			_, err = f.service.Authenticate(t.Context(), current.CookieToken, nil)
			if scenario.revoked != errors.Is(err, ErrUnauthenticated) {
				t.Fatal("unexpected effect on current session")
			}
			if w.Code == http.StatusSeeOther {
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || cookies[0].MaxAge != -1 || w.Header().Get("Location") != "/login" {
					t.Fatal("logout did not clear the stale browser cookie")
				}
			}
		})
	}
}
