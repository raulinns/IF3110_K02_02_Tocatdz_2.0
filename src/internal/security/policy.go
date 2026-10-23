package security

import "context"

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

type Principal struct {
	UserID      ID     `json:"-"`
	SessionID   ID     `json:"-"`
	AuthVersion uint64 `json:"-"`
	CSRFEpoch   uint64 `json:"-"`
}

type Identity struct {
	UserID        ID     `json:"user_id"`
	Username      string `json:"username"`
	PlatformAdmin bool   `json:"platform_admin"`
}

// Cek akses dari data backend terbaru, termasuk untuk admin yang tetap perlu grant database.
type Authorizer interface {
	RequirePlatformAdmin(context.Context, Principal) error
	RequireDatabase(context.Context, Principal, ID, DatabaseAction) error
}

type AccessSnapshot struct {
	Account     Account
	GroupAccess []DatabaseAccess
}

type AccessRepository interface {
	AccessForDatabase(ctx context.Context, userID, databaseID ID) (AccessSnapshot, error)
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok && principal.UserID != "" && principal.SessionID != "" && principal.AuthVersion > 0 && principal.CSRFEpoch > 0
}
