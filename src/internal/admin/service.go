package admin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	MaxNameRunes = 64
	MaxDSNBytes  = 4 << 10
)

type User struct {
	ID            security.ID
	Username      string
	Active        bool
	PlatformAdmin bool
}

type Group struct {
	ID      security.ID
	Name    string
	Members []User
}

// DSN koneksi tidak pernah ikut dikembalikan dari repository.
type Connection struct {
	ID     security.ID
	Name   string
	Engine dbms.Engine
}

type Grant struct {
	GroupID      security.ID
	GroupName    string
	DatabaseID   security.ID
	DatabaseName string
	Access       security.DatabaseAccess
}

type NewUser struct {
	Username      string
	Password      security.Secret
	PlatformAdmin bool
}

// Setiap mutasi wajib atomik: perubahan data, kenaikan AuthVersion akun terdampak, dan pencabutan session untuk pengurangan hak
// terjadi dalam satu transaksi. Repository juga memeriksa ulang bahwa actor masih admin aktif dengan epoch yang sama.
type Repository interface {
	Users(context.Context) ([]User, error)
	Groups(context.Context) ([]Group, error)
	Connections(context.Context) ([]Connection, error)
	Grants(context.Context) ([]Grant, error)
	CreateUser(ctx context.Context, actor security.Principal, user User, passwordHash security.Secret) error
	SetUserActive(ctx context.Context, actor security.Principal, userID security.ID, active bool) error
	CreateGroup(ctx context.Context, actor security.Principal, group Group) error
	AddMember(ctx context.Context, actor security.Principal, groupID security.ID, username string) error
	RemoveMember(ctx context.Context, actor security.Principal, groupID, userID security.ID) error
	CreateConnection(ctx context.Context, actor security.Principal, connection Connection, dsn security.Secret) error
	SetGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) error
	RevokeGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID) error
}

type Authorizer interface {
	RequirePlatformAdmin(context.Context, security.Principal) error
}

// ConnectionChecker memastikan DSN bisa dihubungi sebelum disimpan.
type ConnectionChecker interface {
	Check(context.Context, dbms.Engine, security.Secret) error
}

type IDs interface{ GenerateID() (security.ID, error) }

type Config struct {
	Repository Repository
	Authorizer Authorizer
	Passwords  security.PasswordHasher
	IDs        IDs
	Checker    ConnectionChecker
}

type Service struct{ deps Config }

func NewService(cfg Config) (*Service, error) {
	for _, dependency := range []any{cfg.Repository, cfg.Authorizer, cfg.Passwords, cfg.IDs, cfg.Checker} {
		if missing(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &Service{deps: cfg}, nil
}

func missing(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// Hanya kategori aman dari paket security yang keluar dari service ini.
func safeError(err error) error {
	if err == nil {
		return nil
	}
	// Alasan password dan setup koneksi dipertahankan supaya pesan di UI jelas.
	for _, reason := range []error{security.ErrPasswordTooShort, security.ErrPasswordTooLong, security.ErrPasswordInvalidEncoding, security.ErrTargetNotReady} {
		if errors.Is(err, reason) {
			return errors.Join(security.ErrInvalidInput, reason)
		}
	}
	for _, category := range []error{context.Canceled, context.DeadlineExceeded, security.ErrUnauthenticated, security.ErrForbidden,
		security.ErrNotFound, security.ErrConflict, security.ErrInvalidInput} {
		if errors.Is(err, category) {
			return category
		}
	}
	return security.ErrUnavailable
}

func (s *Service) require(ctx context.Context, actor security.Principal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return safeError(s.deps.Authorizer.RequirePlatformAdmin(ctx, actor))
}

func validName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= MaxNameRunes && !strings.ContainsRune(name, 0)
}

func (s *Service) Users(ctx context.Context, actor security.Principal) ([]User, error) {
	if err := s.require(ctx, actor); err != nil {
		return nil, err
	}
	users, err := s.deps.Repository.Users(ctx)
	return users, safeError(err)
}

func (s *Service) Groups(ctx context.Context, actor security.Principal) ([]Group, error) {
	if err := s.require(ctx, actor); err != nil {
		return nil, err
	}
	groups, err := s.deps.Repository.Groups(ctx)
	return groups, safeError(err)
}

func (s *Service) Connections(ctx context.Context, actor security.Principal) ([]Connection, error) {
	if err := s.require(ctx, actor); err != nil {
		return nil, err
	}
	connections, err := s.deps.Repository.Connections(ctx)
	return connections, safeError(err)
}

func (s *Service) Grants(ctx context.Context, actor security.Principal) ([]Grant, error) {
	if err := s.require(ctx, actor); err != nil {
		return nil, err
	}
	grants, err := s.deps.Repository.Grants(ctx)
	return grants, safeError(err)
}

func (s *Service) newID() (security.ID, error) {
	id, err := s.deps.IDs.GenerateID()
	if err != nil || !security.ValidID(id) {
		return "", security.ErrUnavailable
	}
	return id, nil
}

func (s *Service) CreateUser(ctx context.Context, actor security.Principal, input NewUser) error {
	_, err := s.CreateUserRecord(ctx, actor, input)
	return err
}

// CreateUserRecord returns the public record produced by the committed create.
func (s *Service) CreateUserRecord(ctx context.Context, actor security.Principal, input NewUser) (User, error) {
	if err := s.require(ctx, actor); err != nil {
		return User{}, err
	}
	username, err := security.NormalizeUsername(input.Username)
	if err != nil {
		return User{}, security.ErrInvalidInput
	}
	if err := security.ValidateNewPassword(input.Password); err != nil {
		return User{}, safeError(err)
	}
	hash, err := s.deps.Passwords.Hash(ctx, input.Password)
	if err != nil {
		return User{}, safeError(err)
	}
	id, err := s.newID()
	if err != nil {
		return User{}, err
	}
	user := User{ID: id, Username: username, Active: true, PlatformAdmin: input.PlatformAdmin}
	if err := s.deps.Repository.CreateUser(ctx, actor, user, hash); err != nil {
		return User{}, safeError(err)
	}
	return user, nil
}

func (s *Service) SetUserActive(ctx context.Context, actor security.Principal, userID security.ID, active bool) error {
	if err := s.require(ctx, actor); err != nil {
		return err
	}
	if !security.ValidID(userID) {
		return security.ErrInvalidInput
	}
	// Admin tidak boleh menonaktifkan dirinya sendiri, supaya platform tidak kehilangan admin terakhir lewat kesalahan klik.
	if userID == actor.UserID && !active {
		return security.ErrConflict
	}
	return safeError(s.deps.Repository.SetUserActive(ctx, actor, userID, active))
}

func (s *Service) CreateGroup(ctx context.Context, actor security.Principal, name string) error {
	_, err := s.CreateGroupRecord(ctx, actor, name)
	return err
}

func (s *Service) CreateGroupRecord(ctx context.Context, actor security.Principal, name string) (Group, error) {
	if err := s.require(ctx, actor); err != nil {
		return Group{}, err
	}
	name, ok := validName(name)
	if !ok {
		return Group{}, security.ErrInvalidInput
	}
	id, err := s.newID()
	if err != nil {
		return Group{}, err
	}
	group := Group{ID: id, Name: name}
	if err := s.deps.Repository.CreateGroup(ctx, actor, group); err != nil {
		return Group{}, safeError(err)
	}
	return group, nil
}

func (s *Service) AddMember(ctx context.Context, actor security.Principal, groupID security.ID, username string) error {
	if err := s.require(ctx, actor); err != nil {
		return err
	}
	username, err := security.NormalizeUsername(username)
	if err != nil || !security.ValidID(groupID) {
		return security.ErrInvalidInput
	}
	return safeError(s.deps.Repository.AddMember(ctx, actor, groupID, username))
}

func (s *Service) RemoveMember(ctx context.Context, actor security.Principal, groupID, userID security.ID) error {
	if err := s.require(ctx, actor); err != nil {
		return err
	}
	if !security.ValidID(groupID) || !security.ValidID(userID) {
		return security.ErrInvalidInput
	}
	return safeError(s.deps.Repository.RemoveMember(ctx, actor, groupID, userID))
}

func (s *Service) CreateConnection(ctx context.Context, actor security.Principal, engine dbms.Engine, name string, dsn security.Secret) error {
	_, err := s.CreateConnectionRecord(ctx, actor, engine, name, dsn)
	return err
}

func (s *Service) CreateConnectionRecord(ctx context.Context, actor security.Principal, engine dbms.Engine, name string, dsn security.Secret) (Connection, error) {
	if err := s.require(ctx, actor); err != nil {
		return Connection{}, err
	}
	engine, err := dbms.ParseEngine(string(engine))
	if err != nil {
		return Connection{}, security.ErrInvalidInput
	}
	name, ok := validName(name)
	if !ok || dsn.Empty() || len(dsn.Reveal()) > MaxDSNBytes || !utf8.ValidString(dsn.Reveal()) {
		return Connection{}, security.ErrInvalidInput
	}
	if err := s.deps.Checker.Check(ctx, engine, dsn); err != nil {
		return Connection{}, safeError(err)
	}
	id, err := s.newID()
	if err != nil {
		return Connection{}, err
	}
	connection := Connection{ID: id, Name: name, Engine: engine}
	if err := s.deps.Repository.CreateConnection(ctx, actor, connection, dsn); err != nil {
		return Connection{}, safeError(err)
	}
	return connection, nil
}

func (s *Service) SetGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) error {
	if err := s.require(ctx, actor); err != nil {
		return err
	}
	if !security.ValidID(groupID) || !security.ValidID(databaseID) || (access != security.AccessViewOnly && access != security.AccessViewEdit) {
		return security.ErrInvalidInput
	}
	return safeError(s.deps.Repository.SetGrant(ctx, actor, groupID, databaseID, access))
}

func (s *Service) RevokeGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID) error {
	if err := s.require(ctx, actor); err != nil {
		return err
	}
	if !security.ValidID(groupID) || !security.ValidID(databaseID) {
		return security.ErrInvalidInput
	}
	return safeError(s.deps.Repository.RevokeGrant(ctx, actor, groupID, databaseID))
}
