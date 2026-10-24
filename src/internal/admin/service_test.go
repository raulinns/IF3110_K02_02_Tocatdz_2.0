package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	actorID  security.ID = "b0c4f0c0-9167-43d6-9089-afd7b9826f0d"
	targetID security.ID = "3f1c2d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	password             = "password admin yang panjang"
)

var actor = security.Principal{UserID: actorID, SessionID: "5ed48df2-bcdd-4b52-b9ad-8d9f6689df55", AuthVersion: 1, CSRFEpoch: 1}

type fakeRepository struct {
	calls []string
	user  User
	hash  security.Secret
	dsn   security.Secret
	err   error
}

func (r *fakeRepository) record(call string) error { r.calls = append(r.calls, call); return r.err }
func (r *fakeRepository) Users(context.Context) ([]User, error) {
	return []User{{ID: actorID, Username: "admin"}}, r.record("users")
}
func (r *fakeRepository) Groups(context.Context) ([]Group, error) { return nil, r.record("groups") }
func (r *fakeRepository) Connections(context.Context) ([]Connection, error) {
	return nil, r.record("connections")
}
func (r *fakeRepository) Grants(context.Context) ([]Grant, error) { return nil, r.record("grants") }
func (r *fakeRepository) CreateUser(_ context.Context, _ security.Principal, user User, hash security.Secret) error {
	r.user, r.hash = user, hash
	return r.record("create-user")
}
func (r *fakeRepository) SetUserActive(context.Context, security.Principal, security.ID, bool) error {
	return r.record("set-active")
}
func (r *fakeRepository) CreateGroup(context.Context, security.Principal, Group) error {
	return r.record("create-group")
}
func (r *fakeRepository) AddMember(context.Context, security.Principal, security.ID, string) error {
	return r.record("add-member")
}
func (r *fakeRepository) RemoveMember(context.Context, security.Principal, security.ID, security.ID) error {
	return r.record("remove-member")
}
func (r *fakeRepository) CreateConnection(_ context.Context, _ security.Principal, _ Connection, dsn security.Secret) error {
	r.dsn = dsn
	return r.record("create-connection")
}
func (r *fakeRepository) SetGrant(context.Context, security.Principal, security.ID, security.ID, security.DatabaseAccess) error {
	return r.record("set-grant")
}
func (r *fakeRepository) RevokeGrant(context.Context, security.Principal, security.ID, security.ID) error {
	return r.record("revoke-grant")
}

type fakeAuthorizer struct{ err error }

func (a fakeAuthorizer) RequirePlatformAdmin(context.Context, security.Principal) error { return a.err }

type fakeHasher struct{}

func (fakeHasher) Hash(_ context.Context, p security.Secret) (security.Secret, error) {
	return security.NewSecret("hashed:" + p.Reveal()), nil
}
func (fakeHasher) Verify(context.Context, security.Secret, security.Secret) (bool, error) {
	return false, nil
}

type fakeIDs struct{}

func (fakeIDs) GenerateID() (security.ID, error) { return targetID, nil }

type fakeChecker struct{ err error }

func (c fakeChecker) Check(context.Context, security.Secret) error { return c.err }

func newTestService(t *testing.T, repo *fakeRepository, authorizer fakeAuthorizer, checker fakeChecker) *Service {
	t.Helper()
	service, err := NewService(Config{Repository: repo, Authorizer: authorizer, Passwords: fakeHasher{}, IDs: fakeIDs{}, Checker: checker})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	var nilRepo *fakeRepository
	for _, cfg := range []Config{
		{Repository: nilRepo, Authorizer: fakeAuthorizer{}, Passwords: fakeHasher{}, IDs: fakeIDs{}, Checker: fakeChecker{}},
		{Repository: &fakeRepository{}, Passwords: fakeHasher{}, IDs: fakeIDs{}, Checker: fakeChecker{}},
		{Repository: &fakeRepository{}, Authorizer: fakeAuthorizer{}, IDs: fakeIDs{}, Checker: fakeChecker{}},
		{Repository: &fakeRepository{}, Authorizer: fakeAuthorizer{}, Passwords: fakeHasher{}, Checker: fakeChecker{}},
		{Repository: &fakeRepository{}, Authorizer: fakeAuthorizer{}, Passwords: fakeHasher{}, IDs: fakeIDs{}},
	} {
		if service, err := NewService(cfg); service != nil || !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing dependency accepted")
		}
	}
}

func TestEveryOperationRequiresPlatformAdmin(t *testing.T) {
	repo := &fakeRepository{}
	s := newTestService(t, repo, fakeAuthorizer{err: security.ErrForbidden}, fakeChecker{})
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { _, err := s.Users(ctx, actor); return err },
		func() error { _, err := s.Groups(ctx, actor); return err },
		func() error { _, err := s.Connections(ctx, actor); return err },
		func() error { _, err := s.Grants(ctx, actor); return err },
		func() error {
			return s.CreateUser(ctx, actor, NewUser{Username: "siti", Password: security.NewSecret(password)})
		},
		func() error { return s.SetUserActive(ctx, actor, targetID, false) },
		func() error { return s.CreateGroup(ctx, actor, "tim") },
		func() error { return s.AddMember(ctx, actor, targetID, "siti") },
		func() error { return s.RemoveMember(ctx, actor, targetID, targetID) },
		func() error { return s.CreateConnection(ctx, actor, "toko", security.NewSecret("postgres://x")) },
		func() error { return s.SetGrant(ctx, actor, targetID, targetID, security.AccessViewOnly) },
		func() error { return s.RevokeGrant(ctx, actor, targetID, targetID) },
	} {
		if err := call(); !errors.Is(err, security.ErrForbidden) {
			t.Fatal("non-admin passed authorization")
		}
	}
	if len(repo.calls) != 0 {
		t.Fatal("repository touched without admin rights")
	}
}

func TestCreateUserNormalizesAndHashes(t *testing.T) {
	repo := &fakeRepository{}
	s := newTestService(t, repo, fakeAuthorizer{}, fakeChecker{})
	if err := s.CreateUser(context.Background(), actor, NewUser{Username: "  Siti ", Password: security.NewSecret(password), PlatformAdmin: true}); err != nil {
		t.Fatal(err)
	}
	if repo.user.Username != "siti" || !repo.user.PlatformAdmin || !repo.user.Active || repo.user.ID != targetID || repo.hash.Reveal() != "hashed:"+password {
		t.Fatalf("user not normalized or hashed: %+v", repo.user)
	}
	err := s.CreateUser(context.Background(), actor, NewUser{Username: "budi", Password: security.NewSecret("pendek")})
	if !errors.Is(err, security.ErrInvalidInput) || !errors.Is(err, security.ErrPasswordTooShort) {
		t.Fatal("password policy reason lost")
	}
	if err := s.CreateUser(context.Background(), actor, NewUser{Username: "a b", Password: security.NewSecret(password)}); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("invalid username accepted")
	}
}

func TestAdminCannotDeactivateSelf(t *testing.T) {
	repo := &fakeRepository{}
	s := newTestService(t, repo, fakeAuthorizer{}, fakeChecker{})
	if err := s.SetUserActive(context.Background(), actor, actorID, false); !errors.Is(err, security.ErrConflict) || len(repo.calls) != 0 {
		t.Fatal("admin deactivated own account")
	}
	if err := s.SetUserActive(context.Background(), actor, actorID, true); err != nil {
		t.Fatal(err)
	}
}

func TestCreateConnectionChecksReachabilityAndKeepsSecret(t *testing.T) {
	repo := &fakeRepository{}
	s := newTestService(t, repo, fakeAuthorizer{}, fakeChecker{err: errors.New("dial canary-secret")})
	err := s.CreateConnection(context.Background(), actor, "toko", security.NewSecret("postgres://u:canary-secret@h/db"))
	if err == nil || strings.Contains(err.Error(), "canary") || len(repo.calls) != 0 {
		t.Fatal("unreachable connection stored or error leaked")
	}
	s = newTestService(t, repo, fakeAuthorizer{}, fakeChecker{})
	if err := s.CreateConnection(context.Background(), actor, " toko ", security.NewSecret("postgres://u:p@h/db")); err != nil || repo.dsn.Reveal() != "postgres://u:p@h/db" {
		t.Fatal("valid connection rejected")
	}
	for _, name := range []string{"", strings.Repeat("n", MaxNameRunes+1)} {
		if err := s.CreateConnection(context.Background(), actor, name, security.NewSecret("postgres://x")); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("invalid connection name accepted")
		}
	}
}

func TestInputValidationAndErrorSanitizing(t *testing.T) {
	repo := &fakeRepository{err: errors.New("pq: duplicate key canary-secret")}
	s := newTestService(t, repo, fakeAuthorizer{}, fakeChecker{})
	ctx := context.Background()
	if err := s.SetGrant(ctx, actor, targetID, targetID, security.AccessNone); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("invalid access level accepted")
	}
	if err := s.AddMember(ctx, actor, "bukan-uuid", "siti"); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("invalid group id accepted")
	}
	if err := s.CreateGroup(ctx, actor, "tim"); !errors.Is(err, security.ErrUnavailable) || strings.Contains(err.Error(), "canary") {
		t.Fatal("repository error leaked")
	}
	repo.err = security.ErrConflict
	if err := s.CreateGroup(ctx, actor, "tim"); !errors.Is(err, security.ErrConflict) {
		t.Fatal("conflict category lost")
	}
}
