package targetdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type fakeCatalog struct {
	target Target
	err    error
}

func (c fakeCatalog) Target(context.Context, security.ID) (Target, error) {
	return c.target, c.err
}

type failingConnector struct {
	engine dbms.Engine
	called bool
	err    error
}

func (c *failingConnector) Engine() dbms.Engine { return c.engine }

func (c *failingConnector) Open(context.Context, dbms.SQLConfig) (*sql.DB, error) {
	c.called = true
	return nil, c.err
}

func TestManagerSelectsConnectorByCatalogEngine(t *testing.T) {
	databaseID := security.ID("11111111-1111-4111-8111-111111111111")
	driverErr := errors.New("private connection failure")
	connector := &failingConnector{engine: dbms.PostgreSQL, err: driverErr}
	registry, err := dbms.NewRegistry[dbms.SQLConnector](connector)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(fakeCatalog{target: Target{
		ID: databaseID, Engine: dbms.PostgreSQL,
		DSN: security.NewSecret("postgres://user:password@db:5432/inventory"), Active: true,
	}}, registry)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = manager.Connection(context.Background(), databaseID)
	if !errors.Is(err, security.ErrUnavailable) || !connector.called {
		t.Fatalf("Connection() error = %v, connector called = %v", err, connector.called)
	}
}
