package dbms

import (
	"errors"
	"strings"
)

var ErrUnsupportedEngine = errors.New("unsupported DBMS engine")

type Engine string

const (
	PostgreSQL Engine = "postgresql"
	MySQL      Engine = "mysql"
)

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
