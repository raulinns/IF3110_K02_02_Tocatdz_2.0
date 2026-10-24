package platformstore

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	platformsqlc "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb/sqlc"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func databaseAccess(raw string) (security.DatabaseAccess, error) {
	access := security.DatabaseAccess(raw)
	switch access {
	case security.AccessViewOnly, security.AccessViewEdit:
		return access, nil
	default:
		return security.AccessNone, security.ErrUnavailable
	}
}

func (s *Store) AccessForDatabase(
	ctx context.Context,
	userID security.ID,
	databaseID security.ID,
) (security.AccessSnapshot, error) {
	if !security.ValidID(userID) || !security.ValidID(databaseID) {
		return security.AccessSnapshot{}, security.ErrInvalidInput
	}
	account, err := s.FindAccount(ctx, userID)
	if err != nil {
		return security.AccessSnapshot{}, err
	}
	rows, err := s.queries.ListDatabaseGrants(ctx, platformsqlc.ListDatabaseGrantsParams{
		UserID:     string(userID),
		DatabaseID: string(databaseID),
	})
	if err != nil {
		return security.AccessSnapshot{}, repositoryError(err)
	}
	grants := make([]security.DatabaseAccess, 0, len(rows))
	for _, raw := range rows {
		grant, err := databaseAccess(raw)
		if err != nil {
			return security.AccessSnapshot{}, err
		}
		grants = append(grants, grant)
	}
	return security.AccessSnapshot{Account: account, GroupAccess: grants}, nil
}

func (s *Store) GrantGroupAccess(
	ctx context.Context,
	groupID security.ID,
	databaseID security.ID,
	access security.DatabaseAccess,
) error {
	if !security.ValidID(groupID) || !security.ValidID(databaseID) ||
		(access != security.AccessViewOnly && access != security.AccessViewEdit) {
		return security.ErrInvalidInput
	}
	err := s.queries.UpsertGroupDatabaseAccess(ctx, platformsqlc.UpsertGroupDatabaseAccessParams{
		GroupID:     string(groupID),
		DatabaseID:  string(databaseID),
		AccessLevel: string(access),
	})
	return repositoryError(err)
}

func (s *Store) ListAccessible(
	ctx context.Context,
	principal security.Principal,
) ([]explorer.DatabaseSummary, error) {
	if !security.ValidID(principal.UserID) {
		return nil, security.ErrInvalidInput
	}
	rows, err := s.queries.ListAccessibleDatabases(ctx, string(principal.UserID))
	if err != nil {
		return nil, repositoryError(err)
	}
	result := make([]explorer.DatabaseSummary, 0, len(rows))
	for _, row := range rows {
		access, err := databaseAccess(row.AccessLevel)
		if err != nil || !security.ValidID(security.ID(row.ID)) || row.Name == "" {
			return nil, security.ErrUnavailable
		}
		result = append(result, explorer.DatabaseSummary{
			ID:     security.ID(row.ID),
			Name:   row.Name,
			Access: access,
		})
	}
	return result, nil
}
