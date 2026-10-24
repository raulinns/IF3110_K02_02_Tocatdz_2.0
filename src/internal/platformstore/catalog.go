package platformstore

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	platformsqlc "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb/sqlc"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetdb"
)

type TargetCatalog struct {
	store     *Store
	protector *dbms.ConnectionProtector
}

type ManagedDatabaseInput struct {
	ID     security.ID
	Name   string
	Engine dbms.Engine
	DSN    security.Secret
	Active bool
}

var _ targetdb.Catalog = (*TargetCatalog)(nil)

func NewTargetCatalog(
	store *Store,
	protector *dbms.ConnectionProtector,
) (*TargetCatalog, error) {
	if store == nil || protector == nil {
		return nil, security.ErrInvalidInput
	}
	return &TargetCatalog{store: store, protector: protector}, nil
}

func (c *TargetCatalog) Register(ctx context.Context, input ManagedDatabaseInput) error {
	name := strings.TrimSpace(input.Name)
	engine, err := dbms.ParseEngine(string(input.Engine))
	if err != nil || !security.ValidID(input.ID) || name == "" || len(name) > 120 ||
		!utf8.ValidString(name) || input.DSN.Empty() {
		return security.ErrInvalidInput
	}
	ciphertext, err := c.protector.Seal(ctx, input.ID, input.DSN)
	if err != nil {
		return err
	}
	err = c.store.queries.CreateManagedDatabase(ctx, platformsqlc.CreateManagedDatabaseParams{
		ID:               string(input.ID),
		Name:             name,
		Engine:           string(engine),
		ConnectionSecret: ciphertext,
		Active:           input.Active,
	})
	return repositoryError(err)
}

func (c *TargetCatalog) Target(
	ctx context.Context,
	databaseID security.ID,
) (targetdb.Target, error) {
	if !security.ValidID(databaseID) {
		return targetdb.Target{}, security.ErrInvalidInput
	}
	row, err := c.store.queries.GetManagedDatabase(ctx, string(databaseID))
	if err != nil {
		return targetdb.Target{}, repositoryError(err)
	}
	engine, err := dbms.ParseEngine(row.Engine)
	if err != nil || !security.ValidID(security.ID(row.ID)) || len(row.ConnectionSecret) == 0 {
		return targetdb.Target{}, security.ErrUnavailable
	}
	dsn, err := c.protector.Open(ctx, databaseID, row.ConnectionSecret)
	if err != nil {
		return targetdb.Target{}, err
	}
	return targetdb.Target{
		ID:     security.ID(row.ID),
		Name:   row.Name,
		Engine: engine,
		DSN:    dsn,
		Active: row.Active,
	}, nil
}
