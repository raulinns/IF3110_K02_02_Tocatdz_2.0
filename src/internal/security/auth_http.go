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
	Auth        Authenticator
	Logout      CheckedLogout
	CSRF        CSRFGuard
	Cookies     *SessionCookies
	Throttle    LoginThrottle
	ErrorWriter HTTPErrorWriter
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
	if cfg.ErrorWriter == nil {
		cfg.ErrorWriter = func(w http.ResponseWriter, _ *http.Request, err error) { WriteHTTPError(w, err) }
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
			m.writeError(w, r, ErrUnavailable)
			return
		}
		if err := m.deps.CSRF.VerifyOrigin(r); err != nil {
			m.writeError(w, r, err)
			return
		}
		cookie, err := m.deps.Cookies.Read(r)
		if err != nil {
			m.rejectAuthentication(w, r, err)
			return
		}
		var check SessionCheck
		if !SafeMethod(r.Method) {
			token, err := ReadCSRFToken(w, r)
			if err != nil {
				m.writeError(w, r, err)
				return
			}
			check = func(ctx context.Context, principal Principal) error {
				return m.deps.CSRF.VerifyToken(ctx, principal, token)
			}
		}
		result, err := m.deps.Auth.Authenticate(r.Context(), cookie, check)
		if err != nil {
			m.rejectAuthentication(w, r, err)
			return
		}
		if err := m.deps.Cookies.Write(w, result.CookieToken); err != nil {
			m.writeError(w, r, err)
			return
		}
		ctx := WithPrincipal(r.Context(), result.Principal)
		ctx = context.WithValue(ctx, requestViewKey{}, requestView{identity: result.Identity, csrf: result.CSRFToken})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *HTTPMiddleware) rejectAuthentication(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrUnauthenticated) {
		_ = m.deps.Cookies.Clear(w)
	}
	m.writeError(w, r, err)
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
			m.writeError(w, r, ErrMethodNotAllowed)
			return
		}
		if next == nil {
			m.writeError(w, r, ErrUnavailable)
			return
		}
		if err := m.deps.CSRF.VerifyOrigin(r); err != nil {
			m.writeError(w, r, err)
			return
		}
		if r.URL.RawQuery != "" || !loginFormType(r) || r.ContentLength > MaxLoginFormBytes || r.PostForm != nil || r.Body == nil {
			m.writeError(w, r, ErrInvalidInput)
			return
		}
		// RemoteAddr dipakai langsung supaya header proxy dari client tidak bisa mengakali batas percobaan login.
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			m.writeError(w, r, ErrInvalidInput)
			return
		}
		if err := m.deps.Throttle.CheckIP(r.Context(), ip); err != nil {
			m.writeError(w, r, err)
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
			m.writeError(w, r, ErrMethodNotAllowed)
			return
		}
		if err := m.deps.CSRF.VerifyOrigin(r); err != nil {
			m.writeError(w, r, err)
			return
		}
		cookie, err := m.deps.Cookies.Read(r)
		if err != nil && !errors.Is(err, ErrUnauthenticated) {
			m.writeError(w, r, err)
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
			m.writeError(w, r, err)
			return
		}
		if err := m.deps.Cookies.Clear(w); err != nil {
			m.writeError(w, r, err)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func (m *HTTPMiddleware) writeError(w http.ResponseWriter, r *http.Request, err error) {
	errorHeaders(w, err)
	status, _ := HTTPStatus(err)
	response := &errorResponseWriter{ResponseWriter: w, status: status, err: err, allow: w.Header().Get("Allow")}
	m.deps.ErrorWriter(response, r, err)
	if !response.wroteHeader {
		response.WriteHeader(status)
	}
}

type errorResponseWriter struct {
	http.ResponseWriter
	status      int
	err         error
	allow       string
	wroteHeader bool
}

// Renderer cukup mengurus tampilan, sementara status dan header keamanan tetap dijaga di sini.
func (w *errorResponseWriter) WriteHeader(_ int) {
	if w.wroteHeader {
		return
	}
	errorHeaders(w.ResponseWriter, w.err)
	if w.allow != "" {
		w.Header().Set("Allow", w.allow)
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(w.status)
}

func (w *errorResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(w.status)
	}
	return w.ResponseWriter.Write(body)
}

func (w *errorResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
