package security

import (
	"context"
	"errors"
	"mime"
	"net"
	"net/http"
)

const MaxLoginFormBytes int64 = 16 << 10

type HTTPMiddlewareConfig struct {
	Auth     Authenticator
	Logout   CheckedLogout
	CSRF     CSRFGuard
	Cookies  *SessionCookies
	Throttle LoginThrottle
}

type HTTPMiddleware struct{ deps HTTPMiddlewareConfig }

var _ Middleware = (*HTTPMiddleware)(nil)

func NewHTTPMiddleware(cfg HTTPMiddlewareConfig) (*HTTPMiddleware, error) {
	for _, dependency := range []any{cfg.Auth, cfg.Logout, cfg.CSRF, cfg.Cookies, cfg.Throttle} {
		if missingDependency(dependency) {
			return nil, ErrInvalidInput
		}
	}
	if !cfg.Cookies.configured {
		return nil, ErrInvalidInput
	}
	return &HTTPMiddleware{deps: cfg}, nil
}

type requestView struct {
	identity Identity
	csrf     Secret
}

type requestViewKey struct{}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	view, ok := ctx.Value(requestViewKey{}).(requestView)
	return view.identity, ok
}

func CSRFTokenFromContext(ctx context.Context) (Secret, bool) {
	view, ok := ctx.Value(requestViewKey{}).(requestView)
	return view.csrf, ok && !view.csrf.Empty()
}

func (m *HTTPMiddleware) Protected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if next == nil {
			WriteHTTPError(w, ErrUnavailable)
			return
		}
		if err := m.deps.CSRF.VerifyOrigin(r); err != nil {
			WriteHTTPError(w, err)
			return
		}
		cookie, err := m.deps.Cookies.Read(r)
		if err != nil {
			m.rejectAuthentication(w, err)
			return
		}
		var check SessionCheck
		if !SafeMethod(r.Method) {
			token, err := ReadCSRFToken(w, r)
			if err != nil {
				WriteHTTPError(w, err)
				return
			}
			check = func(ctx context.Context, principal Principal) error {
				return m.deps.CSRF.VerifyToken(ctx, principal, token)
			}
		}
		result, err := m.deps.Auth.Authenticate(r.Context(), cookie, check)
		if err != nil {
			m.rejectAuthentication(w, err)
			return
		}
		if err := m.deps.Cookies.Write(w, result.CookieToken); err != nil {
			WriteHTTPError(w, err)
			return
		}
		ctx := WithPrincipal(r.Context(), result.Principal)
		ctx = context.WithValue(ctx, requestViewKey{}, requestView{identity: result.Identity, csrf: result.CSRFToken})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *HTTPMiddleware) rejectAuthentication(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrUnauthenticated) {
		_ = m.deps.Cookies.Clear(w)
	}
	WriteHTTPError(w, err)
}

func loginFormType(r *http.Request) bool {
	if len(r.Header.Values("Content-Type")) != 1 {
		return false
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && kind == "application/x-www-form-urlencoded"
}

func (m *HTTPMiddleware) LoginProtected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Metode tidak diizinkan", http.StatusMethodNotAllowed)
			return
		}
		if next == nil {
			WriteHTTPError(w, ErrUnavailable)
			return
		}
		if err := m.deps.CSRF.VerifyOrigin(r); err != nil {
			WriteHTTPError(w, err)
			return
		}
		if r.URL.RawQuery != "" || !loginFormType(r) || r.ContentLength > MaxLoginFormBytes || r.PostForm != nil || r.Body == nil {
			WriteHTTPError(w, ErrInvalidInput)
			return
		}
		// RemoteAddr dipakai langsung supaya header proxy dari client tidak bisa mengakali batas percobaan login.
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			WriteHTTPError(w, ErrInvalidInput)
			return
		}
		if err := m.deps.Throttle.CheckIP(r.Context(), ip); err != nil {
			WriteHTTPError(w, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxLoginFormBytes)
		next.ServeHTTP(w, r)
	})
}

func ReadLoginCredentials(w http.ResponseWriter, r *http.Request) (Credentials, error) {
	if r == nil || r.URL == nil || r.Method != http.MethodPost || r.URL.RawQuery != "" || !loginFormType(r) || r.Body == nil || r.PostForm != nil {
		return Credentials{}, ErrInvalidInput
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxLoginFormBytes)
	if err := r.ParseForm(); err != nil {
		return Credentials{}, ErrInvalidInput
	}
	if len(r.PostForm) != 2 || len(r.PostForm["username"]) != 1 || len(r.PostForm["password"]) != 1 {
		return Credentials{}, ErrInvalidInput
	}
	return Credentials{Username: r.PostForm["username"][0], Password: NewSecret(r.PostForm["password"][0])}, nil
}

func (m *HTTPMiddleware) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Metode tidak diizinkan", http.StatusMethodNotAllowed)
			return
		}
		if err := m.deps.CSRF.VerifyOrigin(r); err != nil {
			WriteHTTPError(w, err)
			return
		}
		cookie, err := m.deps.Cookies.Read(r)
		if err != nil && !errors.Is(err, ErrUnauthenticated) {
			WriteHTTPError(w, err)
			return
		}
		check := func(ctx context.Context, principal Principal) error {
			token, err := ReadCSRFToken(w, r)
			if err != nil {
				return err
			}
			return m.deps.CSRF.VerifyToken(ctx, principal, token)
		}
		if err := m.deps.Logout.LogoutWithCheck(r.Context(), cookie, check); err != nil {
			WriteHTTPError(w, err)
			return
		}
		if err := m.deps.Cookies.Clear(w); err != nil {
			WriteHTTPError(w, err)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func WriteHTTPError(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "Terjadi kesalahan"
	switch {
	case errors.Is(err, ErrInvalidInput):
		status, message = http.StatusBadRequest, "Input tidak valid"
	case errors.Is(err, ErrInvalidCredentials):
		status, message = http.StatusUnauthorized, "Username atau password tidak valid"
	case errors.Is(err, ErrUnauthenticated):
		status, message = http.StatusUnauthorized, "Silakan login kembali"
	case errors.Is(err, ErrForbidden):
		status, message = http.StatusForbidden, "Akses ditolak"
	case errors.Is(err, ErrCSRF):
		status, message = http.StatusForbidden, "Permintaan tidak valid"
	case errors.Is(err, ErrNotFound):
		status, message = http.StatusNotFound, "Data tidak ditemukan"
	case errors.Is(err, ErrConflict):
		status, message = http.StatusConflict, "Data berubah, silakan muat ulang"
	case errors.Is(err, ErrRateLimited):
		status, message = http.StatusTooManyRequests, "Terlalu banyak percobaan"
		w.Header().Set("Retry-After", "60")
	case errors.Is(err, ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusServiceUnavailable, "Layanan sementara tidak tersedia"
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, status)
}
