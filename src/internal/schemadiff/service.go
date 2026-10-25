package schemadiff

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Config struct {
	Reader     Reader
	Validator  Validator
	Authorizer security.Authorizer
}

type Service struct{ deps Config }

var _ Application = (*Service)(nil)

func NewService(cfg Config) (*Service, error) {
	for _, dependency := range []any{cfg.Reader, cfg.Validator, cfg.Authorizer} {
		if dependency == nil {
			return nil, security.ErrInvalidInput
		}
		value := reflect.ValueOf(dependency)
		switch value.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
			if value.IsNil() {
				return nil, security.ErrInvalidInput
			}
		}
	}
	return &Service{deps: cfg}, nil
}

func ValidSchemaName(name string) bool {
	if name == "" || len(name) > MaxSchemaBytes || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return false
	}
	return name != "information_schema" && name != "tocatdz_internal" && !strings.HasPrefix(name, "pg_")
}

func safeError(err error) error {
	for _, category := range []error{security.ErrUnauthenticated, security.ErrForbidden, security.ErrNotFound, security.ErrInvalidInput, security.ErrResultTooLarge} {
		if errors.Is(err, category) {
			return category
		}
	}
	return security.ErrUnavailable
}

func (s *Service) authorize(ctx context.Context, req Request) error {
	if ctx.Err() != nil {
		return security.ErrUnavailable
	}
	p := req.Principal
	if !security.ValidID(p.UserID) || !security.ValidID(p.SessionID) || p.AuthVersion == 0 || p.CSRFEpoch == 0 {
		return security.ErrUnauthenticated
	}
	for _, id := range []security.ID{req.CurrentID, req.DesiredID} {
		if !security.ValidID(id) {
			return security.ErrInvalidInput
		}
		if err := s.deps.Authorizer.RequireDatabase(ctx, p, id, security.ActionViewSchema); err != nil {
			return safeError(err)
		}
	}
	return nil
}

func objectCount(snapshot Snapshot) int {
	count := len(snapshot.Views)
	for _, table := range snapshot.Tables {
		count += 1 + len(table.Columns) + len(table.Constraints) + len(table.Indexes)
	}
	return count
}

// Compare hanya membaca katalog kedua database. Kandidat SQL tidak disimpan; user meninjaunya lewat form draft migration.
func (s *Service) Compare(ctx context.Context, req Request) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, CompareTimeout)
	defer cancel()
	if !ValidSchemaName(req.CurrentSchema) || !ValidSchemaName(req.DesiredSchema) {
		return Result{}, security.ErrInvalidInput
	}
	if err := s.authorize(ctx, req); err != nil {
		return Result{}, err
	}
	if req.CurrentID == req.DesiredID && req.CurrentSchema == req.DesiredSchema {
		return Result{}, security.ErrInvalidInput
	}
	current, err := s.deps.Reader.Snapshot(ctx, req.CurrentID, req.CurrentSchema)
	if err != nil {
		return Result{}, safeError(err)
	}
	desired, err := s.deps.Reader.Snapshot(ctx, req.DesiredID, req.DesiredSchema)
	if err != nil {
		return Result{}, safeError(err)
	}
	for _, snapshot := range []Snapshot{current, desired} {
		if snapshot.Quoted == "" || len(snapshot.Tables) > MaxTables || objectCount(snapshot) > MaxObjects {
			return Result{}, security.ErrResultTooLarge
		}
	}
	if current.Schema != req.CurrentSchema || desired.Schema != req.DesiredSchema {
		return Result{}, security.ErrUnavailable
	}
	if err := s.authorize(ctx, req); err != nil {
		return Result{}, err
	}
	changes, warnings, sql := Compare(current, desired)
	if len(changes) > MaxDisplayChange || len(sql) > MaxGeneratedSQL {
		return Result{}, security.ErrResultTooLarge
	}
	result := Result{Current: current, Desired: desired, Changes: changes, Warnings: warnings}
	for _, change := range changes {
		if change.Destructive {
			result.Destructive++
		}
	}
	if sql == "" {
		return result, nil
	}
	sql = "-- Kandidat migration dari schema diff: schema " + current.Quoted + " disamakan dengan schema " + desired.Quoted + " pada pembanding.\n" +
		"-- Tinjau sebelum disimpan. Rename terbaca sebagai hapus lalu tambah, sehingga data kolom atau tabel lama bisa hilang.\n\n" + sql
	qualified, err := s.deps.Validator.QualifyFunctions(ctx, security.NewSecret(sql))
	if err != nil {
		var reason migration.SQLRejection
		if !errors.As(err, &reason) {
			return Result{}, safeError(err)
		}
		qualified = security.NewSecret(sql)
	}
	if len(qualified.Reveal()) > MaxGeneratedSQL {
		return Result{}, security.ErrResultTooLarge
	}
	result.SQL = qualified
	if err := s.deps.Validator.Validate(ctx, qualified); err != nil {
		var reason migration.SQLRejection
		if !errors.As(err, &reason) {
			return Result{}, safeError(err)
		}
		result.Validation = reason.Message()
	}
	return result, nil
}
