package ui

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type AuthMiddleware interface {
	security.Middleware
	LogoutHandler() http.Handler
}

type AuthUI struct {
	auth     security.Authenticator
	guard    AuthMiddleware
	cookies  *security.SessionCookies
	renderer *Renderer
}

var _ security.RouteRegistrar = (*AuthUI)(nil)

type LoginView struct {
	Username string
	Error    string
}

func missingDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func NewAuthUI(auth security.Authenticator, guard AuthMiddleware, cookies *security.SessionCookies, renderer *Renderer) (*AuthUI, error) {
	for _, dependency := range []any{auth, guard, cookies, renderer} {
		if missingDependency(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &AuthUI{auth: auth, guard: guard, cookies: cookies, renderer: renderer}, nil
}

func (ui *AuthUI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", ui.showLogin)
	mux.Handle("POST /login", ui.guard.LoginProtected(http.HandlerFunc(ui.login)))
	mux.Handle("POST /logout", ui.guard.LogoutHandler())
}

func (ui *AuthUI) showLogin(w http.ResponseWriter, r *http.Request) {
	_ = ui.renderer.Render(w, r, http.StatusOK, "login", Page{Title: "Masuk", Content: LoginView{}})
}

func (ui *AuthUI) login(w http.ResponseWriter, r *http.Request) {
	credentials, err := security.ReadLoginCredentials(w, r)
	if err != nil {
		ui.loginFailed(w, r, err, "")
		return
	}
	// cookie lama dipake service untuk menonaktifkan session sebelumnya
	previous, _ := ui.cookies.Read(r)
	result, err := ui.auth.Login(r.Context(), credentials, previous)
	if err != nil {
		ui.loginFailed(w, r, err, credentials.Username)
		return
	}
	if err := ui.cookies.Write(w, result.CookieToken); err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

// password tidak diisi ulang ke form
func (ui *AuthUI) loginFailed(w http.ResponseWriter, r *http.Request, err error, username string) {
	status, message := errorResponse(err)
	if errors.Is(err, security.ErrRateLimited) {
		w.Header().Set("Retry-After", "60")
	}
	_ = ui.renderer.Render(w, r, status, "login", Page{Title: "Masuk", Content: LoginView{Username: username, Error: message}})
}
