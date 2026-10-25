package dbms

import (
	"context"
	"errors"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var ErrUnsupportedEngine = errors.New("unsupported DBMS engine")

type Engine string

const (
	MySQL      Engine = "mysql"
	PostgreSQL Engine = "postgresql"
)

type Capabilities struct {
	QueryPlans bool
	SchemaDiff bool
}

type CapabilityResolver interface {
	Capabilities(context.Context, security.ID) (Capabilities, error)
}

func Features(engine Engine) Capabilities {
	if engine == PostgreSQL {
		return Capabilities{QueryPlans: true, SchemaDiff: true}
	}
	return Capabilities{}
}

func ParseEngine(raw string) (Engine, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))

	switch normalized {
	case "postgres", string(PostgreSQL):
		return PostgreSQL, nil
	case string(MySQL):
		return MySQL, nil

	default:
		return "", ErrUnsupportedEngine
	}
}
