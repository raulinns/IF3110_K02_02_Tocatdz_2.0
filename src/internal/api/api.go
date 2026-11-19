// Package api provides the K1 JSON transport. Business endpoints are added by
// API-02 onward; this package does not assemble stores or start a server/worker.
package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const Prefix = "/api/v1"

type Access uint8

const (
	// Zero value fails closed; public routes must explicitly opt in.
	Authenticated Access = iota
	Public
	Login // Origin + IP throttle, but no session/CSRF token before login.
)

// Endpoint returns errors instead of choosing a second HTTP error representation.
// Use explicit response DTOs and WriteJSON, not domain struct reflection.
type Endpoint func(http.ResponseWriter, *http.Request) error

type BodyMode uint8

const (
	NoBody BodyMode = iota
	JSONBody
)

type Route struct {
	Method          string
	Path            string // Relative to Prefix; e.g. /databases/{database_id}.
	Access          Access
	Body            BodyMode
	QueryParameters []string // Explicit allowlist; duplicate/unknown query keys fail closed.
	Handle          Endpoint
}

type Config struct {
	Security security.HTTPMiddlewareConfig
	Logger   *slog.Logger
	Routes   []Route
}

type API struct {
	mux     *http.ServeMux
	guards  *security.HTTPMiddleware
	logger  *slog.Logger
	methods []string
	handler http.Handler
}

var _ security.RouteRegistrar = (*API)(nil)
var _ http.Handler = (*API)(nil)

// New freezes routes at construction; the resulting handler can serve requests
// concurrently. Transport overrides cannot be weakened by an HTML ErrorWriter.
func New(cfg Config) (*API, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("API logger is required: %w", security.ErrInvalidInput)
	}
	securityConfig := cfg.Security
	securityConfig.Transport = security.JSONTransport
	securityConfig.ErrorWriter = WriteError
	securityConfig.ErrorStatus = func(err error) int { status, _ := errorDetail(err); return status }
	guards, err := security.NewHTTPMiddleware(securityConfig)
	if err != nil {
		return nil, err
	}
	a := &API{mux: http.NewServeMux(), guards: guards, logger: cfg.Logger}
	for _, route := range cfg.Routes {
		if err := a.register(route); err != nil {
			return nil, err
		}
	}
	a.handler = security.SecurityHeaders(http.HandlerFunc(a.serve))
	return a, nil
}

func (a *API) RegisterRoutes(mux *http.ServeMux) {
	// Exact root avoids ServeMux's implicit /api/v1 -> /api/v1/ HTML redirect.
	mux.Handle(Prefix, a)
	mux.Handle(Prefix+"/", a)
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

func (a *API) register(route Route) (err error) {
	route.QueryParameters = append([]string(nil), route.QueryParameters...)
	if route.Handle == nil || !strings.HasPrefix(route.Path, "/") || strings.HasPrefix(route.Path, "//") ||
		strings.ContainsAny(route.Path, "?#\r\n\t ") || !allowedMethod(route.Method) ||
		(route.Access != Authenticated && route.Access != Public && route.Access != Login) ||
		(route.Access == Login && (route.Method != http.MethodPost || route.Body != JSONBody)) ||
		(route.Access == Public && !security.SafeMethod(route.Method)) ||
		(route.Body != NoBody && route.Body != JSONBody) ||
		(security.SafeMethod(route.Method) && route.Body != NoBody) {
		return fmt.Errorf("invalid API route configuration: %w", security.ErrInvalidInput)
	}
	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := ParseQuery(r, route.QueryParameters...); err != nil {
			WriteError(w, r, err)
			return
		}
		if err := checkBody(w, r, route.Body); err != nil {
			WriteError(w, r, err)
			return
		}
		if err := route.Handle(w, r); err != nil {
			// serve supplies the tracking writer beneath auth and endpoint wrappers.
			if responseStarted(w) {
				a.logger.ErrorContext(r.Context(), "API response interrupted", "request_id", security.RequestIDFromContext(r.Context()))
				panic(http.ErrAbortHandler)
			}
			WriteError(w, r, err)
			return
		}
		if !responseStarted(w) {
			// A successful endpoint must explicitly choose its response. Silent
			// empty 200s violate the JSON contract and hide forgotten writes.
			WriteError(w, r, nil)
		}
	}))
	switch route.Access {
	case Authenticated:
		handler = a.guards.Protected(handler)
	case Login:
		handler = a.guards.LoginProtected(handler)
	}
	// ServeMux reports malformed/conflicting patterns as panics. Turn constructor
	// errors into ordinary errors; never let invalid setup become a runtime panic.
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("conflicting or malformed API pattern: %w", security.ErrInvalidInput)
		}
	}()
	a.mux.Handle(route.Method+" "+Prefix+route.Path, handler)
	if !slices.Contains(a.methods, route.Method) {
		a.methods = append(a.methods, route.Method)
	}
	return nil
}

func checkBody(w http.ResponseWriter, r *http.Request, mode BodyMode) error {
	if mode == NoBody {
		return RequireNoBody(r)
	}
	if !security.JSONMediaType(r) {
		return security.ErrUnsupportedMediaType
	}
	if r.ContentLength > MaxJSONBodyBytes {
		return security.ErrPayloadTooLarge
	}
	if r.Body == nil {
		return security.ErrMalformedRequest
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	return nil
}

func allowedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
