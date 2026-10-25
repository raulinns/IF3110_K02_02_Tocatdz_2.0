package dbms

import (
	"errors"
	"testing"
)

func TestParseEngineNormalizesSupportedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want Engine
	}{
		{
			name: "canonical PostgreSQL",
			raw:  "postgresql",
			want: PostgreSQL,
		},
		{
			name: "Postgres alias",
			raw:  "postgres",
			want: PostgreSQL,
		},
		{
			name: "PostgreSQL with different casing",
			raw:  "POSTGRESQL",
			want: PostgreSQL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseEngine(tt.raw)
			if err != nil {
				t.Fatalf(
					"ParseEngine(%q) returned error: %v",
					tt.raw,
					err,
				)
			}

			if got != tt.want {
				t.Fatalf(
					"ParseEngine(%q) = %q, want %q",
					tt.raw,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestParseEngineRejectsUnsupportedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "empty",
			raw:  "",
		},
		{
			name: "whitespace",
			raw:  "   ",
		},
		{
			name: "MariaDB is not supported",
			raw:  "mariadb",
		},
		{
			name: "SQLite is not supported",
			raw:  "sqlite",
		},
		{
			name: "Oracle is not supported",
			raw:  "oracle",
		},
		{
			name: "MySQL is not supported",
			raw:  "mysql",
		},
		{
			name: "PostgreSQL DSN is not an engine name",
			raw:  "postgresql://user:secret@localhost/database",
		},
		{
			name: "MySQL DSN is not an engine name",
			raw:  "user:secret@tcp(localhost:3306)/database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseEngine(tt.raw)

			if !errors.Is(err, ErrUnsupportedEngine) {
				t.Fatalf(
					"ParseEngine(%q) error = %v, want ErrUnsupportedEngine",
					tt.raw,
					err,
				)
			}

			if got != "" {
				t.Fatalf(
					"ParseEngine(%q) = %q, want empty engine",
					tt.raw,
					got,
				)
			}
		})
	}
}
