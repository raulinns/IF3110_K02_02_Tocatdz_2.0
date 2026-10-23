package security

import (
	"context"
	"net/http"
)

// CSRFGuard protects state-changing requests before handlers perform effects.
// VerifyOrigin uses configured public origin, never untrusted Host/forwarded
// headers. VerifyToken uses constant-time comparison of a session-bound HMAC.
// token is from POST form field csrf_token OR X-CSRF-Token, never URL/query.
type CSRFGuard interface {
	VerifyOrigin(r *http.Request) error
	VerifyToken(ctx context.Context, principal Principal, token Secret) error
}

// Middleware is the shared port used by UI/domain route modules. Protected
// verifies Origin on unsafe methods, validates the session with a CSRF
// SessionCheck before Advance, then installs Principal and invokes next.
// It sends Cache-Control: no-store.
// LoginProtected requires same-origin verification on POST /login, without an
// authenticated-session token. Neither wrapper performs domain authorization.
type Middleware interface {
	Protected(next http.Handler) http.Handler
	LoginProtected(next http.Handler) http.Handler
}

// RouteRegistrar is implemented by each HTTP module; only 13524044 calls it
// from the composition root. Each module owns its documented path patterns.
type RouteRegistrar interface {
	RegisterRoutes(mux *http.ServeMux)
}
