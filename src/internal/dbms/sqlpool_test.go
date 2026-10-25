package dbms

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSQLPool struct {
	pingErr       error
	pingCalls     int
	closed        bool
	pingDeadline  time.Time
	pingRemaining time.Duration
}

func (p *fakeSQLPool) PingContext(ctx context.Context) error {
	p.pingCalls++
	p.pingDeadline, _ = ctx.Deadline()
	p.pingRemaining = time.Until(p.pingDeadline)
	return p.pingErr
}

func (p *fakeSQLPool) Close() error {
	p.closed = true
	return nil
}

func TestOpenSQLPoolValidatesAndPingsConnection(t *testing.T) {
	const dsn = "app:secret@tcp(localhost:3306)/target"

	fake := &fakeSQLPool{}
	var gotDriver, gotDSN string

	got, err := openSQLPool(
		context.Background(),
		"fakedriver",
		SQLConfig{DSN: "  " + dsn + "  "},
		func(driverName, databaseDSN string) (*fakeSQLPool, error) {
			gotDriver, gotDSN = driverName, databaseDSN
			return fake, nil
		},
	)
	if err != nil {
		t.Fatalf("openSQLPool() returned error: %v", err)
	}
	if got != fake || gotDriver != "fakedriver" || gotDSN != dsn {
		t.Fatal("openSQLPool() did not pass normalized connection settings")
	}
	if fake.pingCalls != 1 || fake.pingDeadline.IsZero() || fake.closed {
		t.Fatal("openSQLPool() did not perform a bounded startup ping")
	}
}

func TestOpenSQLPoolUsesConfiguredTimeout(t *testing.T) {
	fake := &fakeSQLPool{}
	_, err := openSQLPool(
		context.Background(),
		"fakedriver",
		SQLConfig{
			DSN:            "app:secret@tcp(localhost:3306)/target",
			StartupTimeout: 2 * time.Second,
		},
		func(string, string) (*fakeSQLPool, error) { return fake, nil },
	)
	if err != nil {
		t.Fatalf("openSQLPool() returned error: %v", err)
	}

	if fake.pingRemaining <= 0 || fake.pingRemaining > 2*time.Second {
		t.Fatalf("startup timeout = %v, want within two seconds", fake.pingRemaining)
	}
}

func TestOpenSQLPoolRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		driver string
		cfg    SQLConfig
		want   error
	}{
		{name: "missing driver", cfg: SQLConfig{DSN: "dsn"}, want: ErrDriverRequired},
		{name: "missing DSN", driver: "fakedriver", want: ErrDSNRequired},
		{name: "negative timeout", driver: "fakedriver", cfg: SQLConfig{DSN: "dsn", StartupTimeout: -time.Second}, want: ErrInvalidStartupTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			got, err := openSQLPool(
				context.Background(),
				test.driver,
				test.cfg,
				func(string, string) (*fakeSQLPool, error) {
					called = true
					return &fakeSQLPool{}, nil
				},
			)
			if !errors.Is(err, test.want) || got != nil || called {
				t.Fatalf("openSQLPool() = (%v, %v, called=%v), want nil, %v, false", got, err, called, test.want)
			}
		})
	}
}

func TestOpenSQLPoolClosesFailedConnectionAndRedactsDSN(t *testing.T) {
	const dsn = "admin:private-password@tcp(localhost:3306)/target"
	driverErr := errors.New("cannot connect using " + dsn)
	fake := &fakeSQLPool{pingErr: driverErr}

	got, err := openSQLPool(
		context.Background(),
		"fakedriver",
		SQLConfig{DSN: dsn},
		func(string, string) (*fakeSQLPool, error) { return fake, nil },
	)
	if !errors.Is(err, driverErr) || got != nil || !fake.closed {
		t.Fatalf("failed ping was not wrapped and cleaned up: pool=%v error=%v closed=%v", got, err, fake.closed)
	}
	if strings.Contains(err.Error(), "private-password") {
		t.Fatal("startup error leaked database credentials")
	}
}

func TestOpenSQLPoolRedactsDSNWhenOpenFails(t *testing.T) {
	const dsn = "admin:private-password@tcp(localhost:3306)/target"
	driverErr := errors.New("invalid DSN " + dsn)

	got, err := openSQLPool(
		context.Background(),
		"fakedriver",
		SQLConfig{DSN: dsn},
		func(string, string) (*fakeSQLPool, error) { return nil, driverErr },
	)
	if !errors.Is(err, driverErr) || got != nil {
		t.Fatalf("open failure was not wrapped: pool=%v error=%v", got, err)
	}
	if strings.Contains(err.Error(), "private-password") {
		t.Fatal("open error leaked database credentials")
	}
}
