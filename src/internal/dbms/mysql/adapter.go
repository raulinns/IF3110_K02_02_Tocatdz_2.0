package mysql

import (
	"context"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
)

const DriverName = "mysql"

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (*Adapter) Engine() dbms.Engine {
	return dbms.MySQL
}

func (*Adapter) Open(ctx context.Context, cfg dbms.SQLConfig) (*sql.DB, error) {
	return dbms.OpenSQLPool(ctx, DriverName, cfg)
}
