package explorer

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Viewer interface {
	ListAccessible(context.Context, security.Principal) ([]DatabaseSummary, error)
	Schemas(context.Context, Request) ([]Schema, error)
	Table(context.Context, Request, string, string) (Table, error)
	Rows(context.Context, Request, RowsQuery) (RowsPage, error)
}

type Authorizer interface {
	RequireAuthenticated(context.Context, security.Principal) error
	RequireDatabase(context.Context, security.Principal, security.ID, security.DatabaseAction) error
}

// Directory hanya mengembalikan metadata publik dengan grant group efektif dari snapshot akun yang masih sama.
type Directory interface {
	ListAccessible(context.Context, security.Principal) ([]DatabaseSummary, error)
}

// Reader wajib memakai transaksi read-only, quote identifier, dan membatasi data sebelum dikirim ke service.
type Reader interface {
	Schemas(context.Context, security.ID) ([]Schema, error)
	Table(context.Context, security.ID, string, string) (Table, error)
	Rows(context.Context, security.ID, string, string, RowReadOptions) (RowBatch, error)
}

var _ Authorizer = (*security.BackendAuthorizer)(nil)
