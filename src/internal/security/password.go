package security

import "context"

const (
	PasswordMemoryKiB   uint32 = 19 * 1024
	PasswordIterations  uint32 = 2
	PasswordParallelism uint8  = 1
	PasswordSaltBytes          = 16
	PasswordKeyBytes           = 32
	MinPasswordRunes           = 15
	MaxPasswordBytes           = 1024
)

type PasswordHasher interface {
	Hash(ctx context.Context, password Secret) (encoded Secret, err error)
	Verify(ctx context.Context, encoded, password Secret) (needsRehash bool, err error)
}

type Account struct {
	ID            ID
	Username      string
	Active        bool
	PlatformAdmin bool
	AuthVersion   uint64
}

type CredentialRecord struct {
	Account      Account
	PasswordHash Secret
}

type CredentialRepository interface {
	FindByUsername(ctx context.Context, canonicalUsername string) (CredentialRecord, error)
	FindAccount(ctx context.Context, userID ID) (Account, error)
	// Rehash hanya mengganti hash kalau data lama masih cocok, tanpa mengubah AuthVersion
	Rehash(ctx context.Context, userID ID, expectedHash, replacementHash Secret, expectedAuthVersion uint64) error
}
