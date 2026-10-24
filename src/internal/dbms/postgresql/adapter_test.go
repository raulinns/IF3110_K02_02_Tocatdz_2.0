package postgresql

import (
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
)

func TestAdapterRegistersAsPostgreSQLConnector(t *testing.T) {
	adapter := New()
	registry, err := dbms.NewRegistry[dbms.SQLConnector](adapter)
	if err != nil {
		t.Fatalf("NewRegistry() returned error: %v", err)
	}
	got, err := registry.Resolve(dbms.PostgreSQL)
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	if got != adapter {
		t.Fatal("Resolve() did not return the registered PostgreSQL adapter")
	}
}
