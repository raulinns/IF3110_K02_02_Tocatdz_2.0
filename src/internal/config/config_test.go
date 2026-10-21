package config

import (
	"errors"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		appEnv      string
		httpAddr    string
		databaseURL string
		want        Config
		wantErr     error
	}{
		{
			name:        "loads explicit configuration",
			appEnv:      "testing",
			httpAddr:    ":9090",
			databaseURL: "postgres://app:secret@localhost:5433/platform",
			want: Config{
				AppEnv:      "testing",
				HTTPAddr:    ":9090",
				DatabaseURL: "postgres://app:secret@localhost:5433/platform",
			},
		},
		{
			name:        "uses defaults for non-secret values",
			appEnv:      "",
			httpAddr:    "",
			databaseURL: "postgres://app:secret@localhost:5433/platform",
			want: Config{
				AppEnv:      "development",
				HTTPAddr:    ":8080",
				DatabaseURL: "postgres://app:secret@localhost:5433/platform",
			},
		},
		{
			name:        "rejects empty database URL",
			appEnv:      "development",
			httpAddr:    ":8080",
			databaseURL: "",
			wantErr:     ErrDatabaseURLRequired,
		},
		{
			name:        "rejects whitespace database URL",
			appEnv:      "development",
			httpAddr:    ":8080",
			databaseURL: "   ",
			wantErr:     ErrDatabaseURLRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tt.appEnv)
			t.Setenv("HTTP_ADDR", tt.httpAddr)
			t.Setenv("DATABASE_URL", tt.databaseURL)

			got, err := Load()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tt.want {
				t.Errorf("expected config %+v, got %+v", tt.want, got)
			}
		})
	}
}
