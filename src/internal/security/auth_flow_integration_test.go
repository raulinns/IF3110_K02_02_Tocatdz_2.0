package security

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHTTPAuthFlowIntegration(t *testing.T) {
	f := newAuthFixture(t)
	authorizer, _ := NewBackendAuthorizer(f.store, f.store, f.clock, DefaultSessionPolicy())
	f.store.grants[otherID] = []DatabaseAccess{AccessViewOnly}
	mux := http.NewServeMux()
	mux.Handle("POST /login", f.http.LoginProtected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		input, err := ReadLoginCredentials(w, r)
		if err != nil {
			WriteHTTPError(w, err)
			return
		}
		previous, _ := f.cookies.Read(r)
		result, err := f.service.Login(r.Context(), input, previous)
		if err != nil {
			WriteHTTPError(w, err)
			return
		}
		if err := f.cookies.Write(w, result.CookieToken); err != nil {
			WriteHTTPError(w, err)
			return
		}
		http.Redirect(w, r, "/databases", http.StatusSeeOther)
	})))
	type view struct {
		Username string
		CSRF     string
	}
	mux.Handle("GET /databases", f.http.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, _ := IdentityFromContext(r.Context())
		token, _ := CSRFTokenFromContext(r.Context())
		_ = json.NewEncoder(w).Encode(view{Username: identity.Username, CSRF: token.Reveal()})
	})))
	effects := 0
	mux.Handle("POST /edit", f.http.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ := PrincipalFromContext(r.Context())
		if err := authorizer.RequireDatabase(r.Context(), principal, otherID, ActionExecuteMigration); err != nil {
			WriteHTTPError(w, err)
			return
		}
		effects++
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.Handle("GET /admin", f.http.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ := PrincipalFromContext(r.Context())
		if err := authorizer.RequirePlatformAdmin(r.Context(), principal); err != nil {
			WriteHTTPError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.Handle("POST /logout", f.http.LogoutHandler())
	dispatch := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := dispatch(authRequest("POST", "/login", url.Values{"username": {" ALICE "}, "password": {"a valid test password"}}.Encode(), Secret{}))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/databases" || len(w.Result().Cookies()) != 1 {
		t.Fatal("login integration failed")
	}
	cookie := NewSecret(w.Result().Cookies()[0].Value)
	w = dispatch(authRequest("GET", "/databases", "", cookie))
	var initial view
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &initial) != nil || initial.Username != "alice" || initial.CSRF == "" {
		t.Fatal("protected view integration failed")
	}
	if strings.Contains(w.Body.String(), cookie.Reveal()) || strings.Contains(w.Body.String(), "a valid test password") {
		t.Fatal("credentials/session leaked into view")
	}
	w = dispatch(authRequest("POST", "/edit", "csrf_token="+initial.CSRF, cookie))
	if w.Code != http.StatusForbidden || effects != 0 {
		t.Fatal("View Only performed a write")
	}
	w = dispatch(authRequest("GET", "/admin", "", cookie))
	if w.Code != http.StatusForbidden {
		t.Fatal("ordinary user became admin")
	}
	f.clock.add(time.Second)
	record := f.store.accounts[testUserID]
	record.Account.AuthVersion++
	f.store.accounts[testUserID] = record
	f.store.grants[otherID] = []DatabaseAccess{AccessViewEdit}
	w = dispatch(authRequest("POST", "/edit", "csrf_token="+initial.CSRF, cookie))
	if w.Code != http.StatusForbidden || effects != 0 {
		t.Fatal("stale elevation form performed a write")
	}
	w = dispatch(authRequest("GET", "/databases", "", cookie))
	var elevated view
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &elevated) != nil {
		t.Fatal("elevation reload failed")
	}
	cookie = NewSecret(w.Result().Cookies()[0].Value)
	w = dispatch(authRequest("POST", "/edit", "csrf_token="+elevated.CSRF, cookie))
	if w.Code != http.StatusNoContent || effects != 1 {
		t.Fatal("View + Edit could not write")
	}
	w = dispatch(authRequest("POST", "/logout", "csrf_token="+elevated.CSRF, cookie))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatal("logout integration failed")
	}
	w = dispatch(authRequest("GET", "/databases", "", cookie))
	if w.Code != http.StatusUnauthorized {
		t.Fatal("revoked session still accepted")
	}
}

func TestHTTPRotationAndLogoutAcrossServiceInstances(t *testing.T) {
	f := newAuthFixture(t)
	first := f.login(t)
	second, err := NewAuthService(context.Background(), f.service.deps)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.add(15 * time.Minute)
	rotated, err := second.Authenticate(context.Background(), first.CookieToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := f.service.Authenticate(context.Background(), first.CookieToken, nil)
	if err != nil || recovered.CookieToken.Reveal() != rotated.CookieToken.Reveal() {
		t.Fatal("cross-instance grace recovery failed")
	}
	if err := second.Logout(context.Background(), rotated.CookieToken); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.Authenticate(context.Background(), first.CookieToken, nil)
	assertAuthError(t, err, ErrUnauthenticated)
}
