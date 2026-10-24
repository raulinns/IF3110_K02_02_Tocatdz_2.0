package postgresql

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
)

const DriverName = "pgx"

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (*Adapter) Engine() dbms.Engine {
	return dbms.PostgreSQL
}

func (*Adapter) Open(ctx context.Context, cfg dbms.SQLConfig) (*sql.DB, error) {
	return dbms.OpenSQLPool(ctx, DriverName, cfg)
}
