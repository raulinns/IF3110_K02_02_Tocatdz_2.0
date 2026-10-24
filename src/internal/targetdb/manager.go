package targetdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"sync"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Target struct {
	ID     security.ID
	Name   string
	Engine dbms.Engine
	DSN    security.Secret
	Active bool
}

type Catalog interface {
	Target(context.Context, security.ID) (Target, error)
}

type ConnectionResolver interface {
	Connection(context.Context, security.ID) (dbms.Engine, *sql.DB, error)
}

type cachedConnection struct {
	engine      dbms.Engine
	dsnChecksum [32]byte
	pool        *sql.DB
}

type Manager struct {
	catalog  Catalog
	registry *dbms.Registry[dbms.SQLConnector]
	mu       sync.Mutex
	pools    map[security.ID]cachedConnection
	closed   bool
}

func NewManager(
	catalog Catalog,
	registry *dbms.Registry[dbms.SQLConnector],
) (*Manager, error) {
	if catalog == nil || registry == nil {
		return nil, security.ErrInvalidInput
	}
	return &Manager{
		catalog:  catalog,
		registry: registry,
		pools:    make(map[security.ID]cachedConnection),
	}, nil
}

func (m *Manager) Connection(
	ctx context.Context,
	databaseID security.ID,
) (dbms.Engine, *sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if !security.ValidID(databaseID) {
		return "", nil, security.ErrInvalidInput
	}
	target, err := m.catalog.Target(ctx, databaseID)
	if err != nil {
		return "", nil, err
	}
	if target.ID != databaseID || !target.Active || target.DSN.Empty() {
		return "", nil, security.ErrNotFound
	}
	engine, err := dbms.ParseEngine(string(target.Engine))
	if err != nil {
		return "", nil, security.ErrUnavailable
	}
	checksum := sha256.Sum256([]byte(target.DSN.Reveal()))

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", nil, security.ErrUnavailable
	}
	if cached, ok := m.pools[databaseID]; ok && cached.engine == engine && cached.dsnChecksum == checksum {
		m.mu.Unlock()
		return cached.engine, cached.pool, nil
	}
	m.mu.Unlock()

	connector, err := m.registry.Resolve(engine)
	if err != nil {
		return "", nil, security.ErrUnavailable
	}
	pool, err := connector.Open(ctx, dbms.SQLConfig{DSN: target.DSN.Reveal()})
	if err != nil {
		return "", nil, security.ErrUnavailable
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		_ = pool.Close()
		return "", nil, security.ErrUnavailable
	}
	if cached, ok := m.pools[databaseID]; ok && cached.engine == engine && cached.dsnChecksum == checksum {
		_ = pool.Close()
		return cached.engine, cached.pool, nil
	}
	if previous, ok := m.pools[databaseID]; ok {
		_ = previous.pool.Close()
	}
	m.pools[databaseID] = cachedConnection{
		engine:      engine,
		dsnChecksum: checksum,
		pool:        pool,
	}
	return engine, pool, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true

	var result error
	for id, connection := range m.pools {
		result = errors.Join(result, connection.pool.Close())
		delete(m.pools, id)
	}
	return result
}
