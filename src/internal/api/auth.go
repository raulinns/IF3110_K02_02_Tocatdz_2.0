package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"slices"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// GroupLeadership is the consumer-owned subset of migration.LeaderDirectory. The
// database owner can implement both without making auth import migration/worker.
// Return current group IDs whose active leader is userID; errors are not empty lists.
type GroupLeadership interface {
	LeaderGroups(ctx context.Context, userID security.ID) ([]security.ID, error)
}

type AuthConfig struct {
	Leaders GroupLeadership
}

// UserResponse is an explicit OpenAPI DTO, never a serialized Account or Principal.
// Auth responses always have Active=true. Admin responses use the stored state.
type UserResponse struct {
	ID            security.ID `json:"id"`
	Username      string      `json:"username"`
	Active        bool        `json:"active"`
	PlatformAdmin bool        `json:"platform_admin"`
}

type SessionResponse struct {
	User          UserResponse  `json:"user"`
	PlatformAdmin bool          `json:"platform_admin"`
	CSRFToken     string        `json:"csrf_token"`
	LeaderOf      []security.ID `json:"leader_of"`
}

type loginInput struct {
	Username *string `json:"username"`
	Password *string `json:"password"`
}

type authEndpoints struct {
	service security.Authenticator
	cookies *security.SessionCookies
	leaders GroupLeadership
	logger  *slog.Logger
}

func missingAuthDependency(directory any) bool {
	if directory == nil {
		return true
	}
	value := reflect.ValueOf(directory)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

func (a *API) registerAuth(auth *authEndpoints) error {
	for _, route := range []Route{
		{Method: http.MethodPost, Path: "/auth/login", Access: Login, Body: JSONBody, Handle: auth.login},
		{Method: http.MethodGet, Path: "/auth/me", Handle: auth.me},
	} {
		if err := a.register(route); err != nil {
			return err
		}
	}
	// Checked revocation already implements Origin, CSRF, active-session checks,
	// no-body validation, cookie expiry and JSON 204. Avoid a second Protected
	// wrapper, which would unnecessarily rotate/emit a cookie just before logout.
	return a.registerHTTP(http.MethodPost, "/auth/logout", a.guards.LogoutHandler())
}

func (a *authEndpoints) login(w http.ResponseWriter, r *http.Request) error {
	var input loginInput
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	fields := make(map[string][]string)
	if input.Username == nil || *input.Username == "" {
		fields["username"] = []string{"Username wajib diisi"}
	} else if len(*input.Username) > 128 {
		fields["username"] = []string{"Username maksimal 128 byte sebelum normalisasi"}
	}
	if input.Password == nil || *input.Password == "" {
		fields["password"] = []string{"Password wajib diisi"}
	} else if len(*input.Password) > security.MaxPasswordBytes {
		fields["password"] = []string{"Password maksimal 1024 byte"}
	}
	if len(fields) != 0 {
		return &ValidationError{Fields: fields}
	}
	previous, err := a.cookies.Read(r)
	if err != nil && !errors.Is(err, security.ErrUnauthenticated) {
		return security.ErrUnavailable
	}
	result, err := a.service.Login(r.Context(), security.Credentials{
		Username: *input.Username, Password: security.NewSecret(*input.Password),
	}, previous)
	if err != nil {
		logAuth(a.logger, r.Context(), "auth.login", "anonymous", err)
		return err
	}
	// Authentication has committed a new session (and may have replaced an old
	// one). Deliver its cookie even if the subsequent leadership read fails. This
	// avoids an orphan session or stranding the browser with a revoked old cookie.
	if err := a.cookies.Write(w, result.CookieToken); err != nil {
		return security.ErrUnavailable
	}
	logAuth(a.logger, r.Context(), "auth.login", string(result.Identity.UserID), nil)
	view, err := a.sessionView(r.Context(), result.Principal, result.Identity, result.CSRFToken)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, view)
}

func (a *authEndpoints) me(w http.ResponseWriter, r *http.Request) error {
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return security.ErrUnauthenticated
	}
	identity, ok := security.IdentityFromContext(r.Context())
	if !ok {
		return security.ErrUnavailable
	}
	token, ok := security.CSRFTokenFromContext(r.Context())
	if !ok {
		return security.ErrUnavailable
	}
	view, err := a.sessionView(r.Context(), principal, identity, token)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, view)
}

func (a *authEndpoints) sessionView(ctx context.Context, principal security.Principal, identity security.Identity, csrf security.Secret) (SessionResponse, error) {
	username, err := security.NormalizeUsername(identity.Username)
	if err != nil || username != identity.Username || !security.ValidID(identity.UserID) || identity.UserID != principal.UserID || csrf.Empty() {
		return SessionResponse{}, security.ErrUnavailable
	}
	groups, err := a.leaders.LeaderGroups(ctx, identity.UserID)
	if err != nil {
		// An unavailable/misconfigured directory is not proof that the user leads
		// no groups. Its raw errors and internal resources never reach the client.
		return SessionResponse{}, security.ErrUnavailable
	}
	groups = append([]security.ID{}, groups...)
	for _, id := range groups {
		if !security.ValidID(id) {
			return SessionResponse{}, security.ErrUnavailable
		}
	}
	slices.Sort(groups)
	groups = slices.Compact(groups)
	return SessionResponse{
		User:          UserResponse{ID: identity.UserID, Username: identity.Username, Active: true, PlatformAdmin: identity.PlatformAdmin},
		PlatformAdmin: identity.PlatformAdmin, CSRFToken: csrf.Reveal(), LeaderOf: groups,
	}, nil
}

// auditedLogout delegates checked revocation unchanged. The callback captures
// only a verified actor ID; cookies, session IDs and CSRF tokens are never logged.
type auditedLogout struct {
	next   security.CheckedLogout
	logger *slog.Logger
}

func (a *auditedLogout) LogoutWithCheck(ctx context.Context, cookie security.Secret, check security.SessionCheck) error {
	actor := "anonymous"
	checked := false
	err := a.next.LogoutWithCheck(ctx, cookie, func(ctx context.Context, principal security.Principal) error {
		if check == nil {
			return security.ErrCSRF
		}
		if err := check(ctx, principal); err != nil {
			return err
		}
		actor, checked = string(principal.UserID), true
		return nil
	})
	loggedError := err
	if loggedError == nil && !checked {
		loggedError = security.ErrUnauthenticated // JSON logout emits 401 in this case.
	}
	logAuth(a.logger, ctx, "auth.logout", actor, loggedError)
	return err
}

func logAuth(logger *slog.Logger, ctx context.Context, event, actor string, err error) {
	status := "succeeded"
	var code any
	if err != nil {
		status = "failed"
		_, detail := errorDetail(err)
		code = detail.Code
	}
	logger.InfoContext(ctx, "Authentication result", "event", event, "actor", actor,
		"database_id", nil, "status", status, "request_id", security.RequestIDFromContext(ctx),
		"trace_id", nil, "span_id", nil, "error", code)
}
