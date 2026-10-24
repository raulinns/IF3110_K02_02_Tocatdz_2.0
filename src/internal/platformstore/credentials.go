package platformstore

import (
	"context"

	platformsqlc "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb/sqlc"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func account(
	id string,
	username string,
	active bool,
	platformAdmin bool,
	authVersion int64,
) (security.Account, error) {
	version, err := checkedUint64(authVersion)
	if err != nil || !security.ValidID(security.ID(id)) {
		return security.Account{}, security.ErrUnavailable
	}
	return security.Account{
		ID:            security.ID(id),
		Username:      username,
		Active:        active,
		PlatformAdmin: platformAdmin,
		AuthVersion:   version,
	}, nil
}

func (s *Store) FindByUsername(ctx context.Context, username string) (security.CredentialRecord, error) {
	row, err := s.queries.GetUserByUsername(ctx, username)
	if err != nil {
		return security.CredentialRecord{}, repositoryError(err)
	}
	result, err := account(row.ID, row.Username, row.Active, row.PlatformAdmin, row.AuthVersion)
	if err != nil {
		return security.CredentialRecord{}, err
	}
	return security.CredentialRecord{
		Account:      result,
		PasswordHash: security.NewSecret(row.PasswordHash),
	}, nil
}

func (s *Store) FindAccount(ctx context.Context, userID security.ID) (security.Account, error) {
	row, err := s.queries.GetUserByID(ctx, string(userID))
	if err != nil {
		return security.Account{}, repositoryError(err)
	}
	return account(row.ID, row.Username, row.Active, row.PlatformAdmin, row.AuthVersion)
}

func (s *Store) Rehash(
	ctx context.Context,
	userID security.ID,
	expectedHash security.Secret,
	replacementHash security.Secret,
	expectedAuthVersion uint64,
) error {
	version, err := checkedInt64(expectedAuthVersion)
	if err != nil || !security.ValidID(userID) || expectedHash.Empty() || replacementHash.Empty() {
		return security.ErrInvalidInput
	}
	rows, err := s.queries.RehashUserPassword(ctx, platformsqlc.RehashUserPasswordParams{
		ID:             string(userID),
		PasswordHash:   expectedHash.Reveal(),
		PasswordHash_2: replacementHash.Reveal(),
		AuthVersion:    version,
	})
	if err != nil {
		return repositoryError(err)
	}
	if rows != 1 {
		return security.ErrConflict
	}
	return nil
}
