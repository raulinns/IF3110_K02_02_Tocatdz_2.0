package ui

import (
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type DatabaseGuard struct {
	authorizer security.Authorizer
}

type DatabaseRequest struct {
	Principal  security.Principal
	DatabaseID security.ID
}

func NewDatabaseGuard(authorizer security.Authorizer) (*DatabaseGuard, error) {
	if missingDependency(authorizer) {
		return nil, security.ErrInvalidInput
	}
	return &DatabaseGuard{authorizer: authorizer}, nil
}

// Identitas hanya dari context middleware; ID database dari URL divalidasi sebelum authorizer dipanggil
func (g *DatabaseGuard) Authorize(r *http.Request, action security.DatabaseAction) (DatabaseRequest, error) {
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return DatabaseRequest{}, security.ErrUnauthenticated
	}
	databaseID, err := pathID(r, "databaseID")
	if err != nil {
		return DatabaseRequest{}, err
	}
	if err := g.authorizer.RequireDatabase(r.Context(), principal, databaseID, action); err != nil {
		return DatabaseRequest{}, err
	}
	return DatabaseRequest{Principal: principal, DatabaseID: databaseID}, nil
}

// Hanya untuk memilih tampilan; setiap aksi tetap diotorisasi ulang saat request-nya datang
func (g *DatabaseGuard) Allows(r *http.Request, request DatabaseRequest, action security.DatabaseAction) bool {
	return g.authorizer.RequireDatabase(r.Context(), request.Principal, request.DatabaseID, action) == nil
}

func (g *DatabaseGuard) RequireAdmin(r *http.Request) (security.Principal, error) {
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return security.Principal{}, security.ErrUnauthenticated
	}
	if err := g.authorizer.RequirePlatformAdmin(r.Context(), principal); err != nil {
		return security.Principal{}, err
	}
	return principal, nil
}
