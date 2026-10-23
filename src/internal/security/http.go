package security

import (
	"context"
	"net/http"
)

type CSRFGuard interface {
	VerifyOrigin(r *http.Request) error
	VerifyToken(ctx context.Context, principal Principal, token Secret) error
}

type Middleware interface {
	Protected(next http.Handler) http.Handler
	LoginProtected(next http.Handler) http.Handler
}

type RouteRegistrar interface {
	RegisterRoutes(mux *http.ServeMux)
}
