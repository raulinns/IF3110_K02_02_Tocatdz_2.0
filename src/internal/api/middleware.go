package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func (a *API) serve(w http.ResponseWriter, r *http.Request) {
	id := security.RequestIDFromContext(r.Context())
	if id == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			WriteError(w, r, err)
			return
		}
		id = hex.EncodeToString(raw[:])
		r = r.WithContext(security.WithRequestID(r.Context(), id))
	}
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Cache-Control", "no-store")
	tracked := &responseWriter{ResponseWriter: w, head: r.Method == http.MethodHead}
	defer func() {
		if failure := recover(); failure != nil {
			if failure == http.ErrAbortHandler {
				panic(failure)
			}
			// The panic value, URL, headers and bodies can contain credentials.
			a.logger.ErrorContext(r.Context(), "API panic recovered", "request_id", id)
			if tracked.started {
				panic(http.ErrAbortHandler) // Close an incomplete response, never append JSON.
			}
			WriteError(tracked, r, nil)
		}
	}()
	escapedPath := r.URL.EscapedPath()
	clean := path.Clean(escapedPath)
	if escapedPath != clean && escapedPath != clean+"/" {
		WriteError(tracked, r, security.ErrMalformedRequest)
		return
	}
	if r.URL.Path != Prefix && !strings.HasPrefix(r.URL.Path, Prefix+"/") {
		WriteError(tracked, r, security.ErrNotFound)
		return
	}
	if _, pattern := a.mux.Handler(r); pattern != "" {
		a.mux.ServeHTTP(tracked, r)
		return
	}
	// ServeMux's built-in 404/405 handlers are text. Determine Allow using the
	// same pattern matcher, then emit the K1 envelope without buffering responses.
	methods := append(slices.Clone(a.methods), http.MethodHead)
	allowed := []string{}
	for _, method := range methods {
		candidate := r.Clone(r.Context())
		candidate.Method = method
		if _, pattern := a.mux.Handler(candidate); pattern != "" && !slices.Contains(allowed, method) {
			allowed = append(allowed, method)
		}
	}
	if len(allowed) > 0 {
		slices.Sort(allowed)
		tracked.Header().Set("Allow", strings.Join(allowed, ", "))
		WriteError(tracked, r, security.ErrMethodNotAllowed)
		return
	}
	WriteError(tracked, r, security.ErrNotFound)
}

type responseWriter struct {
	http.ResponseWriter
	started bool
	head    bool
}

func (w *responseWriter) WriteHeader(status int) {
	if !w.started {
		w.started = true
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if w.head {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Flush() {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func responseStarted(w http.ResponseWriter) bool {
	if tracked, ok := w.(*responseWriter); ok {
		return tracked.started
	}
	if wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
		return responseStarted(wrapper.Unwrap())
	}
	return false
}
