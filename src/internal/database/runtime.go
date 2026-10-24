package database

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	mysqladapter "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms/mysql"
	postgresqladapter "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms/postgresql"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformstore"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetdb"
)

type Runtime struct {
	Store       *platformstore.Store
	Catalog     *platformstore.TargetCatalog
	Registry    *dbms.Registry[dbms.SQLConnector]
	Connections *targetdb.Manager
	Reader      *targetdb.Reader
}

func NewRuntime(platformPool *pgxpool.Pool, connectionKey []byte) (*Runtime, error) {
	if platformPool == nil {
		return nil, security.ErrInvalidInput
	}
	store, err := platformstore.New(platformPool)
	if err != nil {
		return nil, err
	}
	protector, err := dbms.NewConnectionProtector(connectionKey)
	if err != nil {
		return nil, err
	}
	catalog, err := platformstore.NewTargetCatalog(store, protector)
	if err != nil {
		return nil, err
	}
	registry, err := dbms.NewRegistry[dbms.SQLConnector](
		postgresqladapter.New(),
		mysqladapter.New(),
	)
	if err != nil {
		return nil, err
	}
	connections, err := targetdb.NewManager(catalog, registry)
	if err != nil {
		return nil, err
	}
	reader, err := targetdb.NewReader(connections)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Store:       store,
		Catalog:     catalog,
		Registry:    registry,
		Connections: connections,
		Reader:      reader,
	}, nil
}

func (r *Runtime) Close() error {
	if r == nil || r.Connections == nil {
		return nil
	}
	return r.Connections.Close()
}
