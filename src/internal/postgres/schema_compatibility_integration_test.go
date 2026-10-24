package postgres_test

import (
	"context"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/testutil/pgtest"
)

func TestSchemaFamiliesDoNotOverwriteEachOther(t *testing.T) {
	for _, family := range []string{"application", "foundation"} {
		t.Run(family, func(t *testing.T) {
			url := pgtest.Database(t, "TEST_DATABASE_URL")
			ctx := t.Context()
			pool, err := postgres.Open(ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			first, second := postgres.Migrate, platformdb.Migrate
			if family == "foundation" {
				first, second = second, first
			}
			if err := first(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,password_hash) VALUES ('11111111-1111-4111-8111-111111111111','sentinel','placeholder')`); err != nil {
				t.Fatal(err)
			}
			if err := second(ctx, pool); err == nil {
				t.Fatal("incompatible schema was accepted")
			}
			var count int
			if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM users WHERE username='sentinel'").Scan(&count); err != nil || count != 1 {
				t.Fatal("existing data changed during incompatible migration")
			}
		})
	}
}
