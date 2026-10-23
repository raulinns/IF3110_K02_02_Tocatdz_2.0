package security

import "context"

// ID is an internal UUID string allocated server-side. Validate canonical UUIDs
// at HTTP boundaries; adapters map ID to their native UUID without importing
// generated SQL types into this package.
type ID string

type DatabaseAccess string

const (
	AccessNone     DatabaseAccess = "none"
	AccessViewOnly DatabaseAccess = "view_only"
	AccessViewEdit DatabaseAccess = "view_edit"
)

type DatabaseAction string

const (
	ActionViewSchema       DatabaseAction = "schema.view"
	ActionViewData         DatabaseAction = "data.view"
	ActionViewHistory      DatabaseAction = "migration.history.view"
	ActionViewMigration    DatabaseAction = "migration.view"
	ActionCreateMigration  DatabaseAction = "migration.create"
	ActionUpdateMigration  DatabaseAction = "migration.update"
	ActionExecuteMigration DatabaseAction = "migration.execute"
)

// EffectiveAccess applies maximum privilege over group grants for ONE database.
// Unknown persisted values fail closed rather than silently becoming a grant.
func EffectiveAccess(grants []DatabaseAccess) (DatabaseAccess, error) {
	result := AccessNone
	for _, grant := range grants {
		switch grant {
		case AccessNone:
		case AccessViewOnly:
			if result == AccessNone {
				result = AccessViewOnly
			}
		case AccessViewEdit:
			result = AccessViewEdit
		default:
			return AccessNone, ErrInvalidInput
		}
	}
	return result, nil
}

// Allows is pure policy, NOT an authorization check. Services must use
// Authorizer to load current account status and grants before invoking it.
func Allows(access DatabaseAccess, action DatabaseAction) bool {
	if access != AccessViewOnly && access != AccessViewEdit {
		return false
	}
	switch action {
	case ActionViewSchema, ActionViewData, ActionViewHistory, ActionViewMigration:
		return true
	case ActionCreateMigration, ActionUpdateMigration, ActionExecuteMigration:
		return access == AccessViewEdit
	default:
		return false
	}
}

// Principal is internal authenticated context, never a client-supplied DTO.
// AuthVersion is an account security epoch, not a role or a DB access level.
type Principal struct {
	UserID      ID     `json:"-"`
	SessionID   ID     `json:"-"`
	AuthVersion uint64 `json:"-"`
	CSRFEpoch   uint64 `json:"-"`
}

// Identity is the explicit allowlist exposed to templates or JSON responses.
// PlatformAdmin controls UI visibility only; Authorizer remains authoritative.
type Identity struct {
	UserID        ID     `json:"user_id"`
	Username      string `json:"username"`
	PlatformAdmin bool   `json:"platform_admin"`
}

// Authorizer revalidates active account, session, security epoch, and current
// grants using backend state. Unknown actions/empty IDs deny. Infrastructure
// errors return ErrUnavailable and never fall back to permission cached in UI.
// Platform Admin has NO implicit database grant.
type Authorizer interface {
	RequirePlatformAdmin(context.Context, Principal) error
	RequireDatabase(context.Context, Principal, ID, DatabaseAction) error
}

// AccessSnapshot is loaded consistently from current account/group/grant state.
// GroupAccess contains only grants for the requested database.
type AccessSnapshot struct {
	Account     Account
	GroupAccess []DatabaseAccess
}

type AccessRepository interface {
	AccessForDatabase(ctx context.Context, userID, databaseID ID) (AccessSnapshot, error)
}

type principalKey struct{}

// WithPrincipal is called by authentication middleware, never from form data.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok && principal.UserID != "" && principal.SessionID != "" && principal.AuthVersion > 0 && principal.CSRFEpoch > 0
}
