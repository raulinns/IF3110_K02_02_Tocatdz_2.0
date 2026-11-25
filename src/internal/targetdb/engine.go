package targetdb

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Engine returns catalog metadata without opening a target connection. Callers
// exposing it to users must first authorize the database ID.
func (r *Router) Engine(ctx context.Context, id security.ID) (dbms.Engine, error) {
	return r.engine(ctx, id)
}
