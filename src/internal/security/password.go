package security

import "context"

// Argon2id configuration: memory in KiB, two passes, one lane, 16-byte salt,
// 32-byte key. Encode as PHC $argon2id$v=19$m=19456,t=2,p=1$... .
// Adapter dependency: golang.org/x/crypto/argon2 (added by 13524044).
const (
	PasswordMemoryKiB   uint32 = 19 * 1024
	PasswordIterations  uint32 = 2
	PasswordParallelism uint8  = 1
	PasswordSaltBytes          = 16
	PasswordKeyBytes           = 32
	MinPasswordRunes           = 15
	MaxPasswordBytes           = 1024
)

// PasswordHasher uses a new crypto/rand salt for every Hash. Verify rejects
// malformed/oversized PHC parameters before allocating memory; compare derived
// keys in constant time. Wrong passwords return ErrInvalidCredentials.
// needsRehash is true ONLY after successful verification of an older policy.
// Hash enforces creation policy; Verify must still accept previously valid
// passwords within the resource limit. Do not trim or normalize passwords.
type PasswordHasher interface {
	Hash(ctx context.Context, password Secret) (encoded Secret, err error)
	Verify(ctx context.Context, encoded, password Secret) (needsRehash bool, err error)
}

// Account represents current backend state. AuthVersion starts at 1 and changes
// atomically with security mutations; it is never editable by the client.
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

// CredentialRepository returns ErrNotFound for absent users. Login maps absent,
// inactive, and password mismatch to the SAME ErrInvalidCredentials response
// and performs a dummy hash verification to avoid the absent-user fast path.
type CredentialRepository interface {
	FindByUsername(ctx context.Context, canonicalUsername string) (CredentialRecord, error)
	FindAccount(ctx context.Context, userID ID) (Account, error)
	// Rehash changes representation only (no epoch bump). Compare-and-swap avoids
	// overwriting a password reset; zero matches returns ErrConflict.
	Rehash(ctx context.Context, userID ID, expectedHash, replacementHash Secret, expectedAuthVersion uint64) error
}
