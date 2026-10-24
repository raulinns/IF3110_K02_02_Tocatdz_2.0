package platformdb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakePool struct {
	pingErr     error
	pingCalls   int
	closed      bool
	sawDeadline bool
}

func (p *fakePool) Ping(ctx context.Context) error {
	p.pingCalls++

	_, p.sawDeadline = ctx.Deadline()

	return p.pingErr
}

func (p *fakePool) Close() {
	p.closed = true
}

func TestOpenPoolOpensAndPingsDatabase(t *testing.T) {
	t.Parallel()

	const databaseURL = "postgres://app:secret@localhost:5433/platform"

	fake := &fakePool{}

	var (
		gotURL      string
		sawDeadline bool
	)

	got, err := openPool(
		context.Background(),
		Config{
			URL: "  " + databaseURL + "  ",
		},
		func(ctx context.Context, url string) (*fakePool, error) {
			gotURL = url
			_, sawDeadline = ctx.Deadline()

			return fake, nil
		},
	)
	if err != nil {
		t.Fatalf("openPool() returned error: %v", err)
	}

	if got != fake {
		t.Fatalf("openPool() returned pool %p, want %p", got, fake)
	}

	if gotURL != databaseURL {
		t.Fatalf("opener URL = %q, want %q", gotURL, databaseURL)
	}

	if !sawDeadline {
		t.Fatal("opener context does not have a startup deadline")
	}

	if fake.pingCalls != 1 {
		t.Fatalf("Ping() called %d times, want 1", fake.pingCalls)
	}

	if !fake.sawDeadline {
		t.Fatal("Ping() context does not have a startup deadline")
	}

	if fake.closed {
		t.Fatal("successful pool was closed during startup")
	}
}

func TestOpenPoolUsesConfiguredStartupTimeout(t *testing.T) {
	t.Parallel()

	const startupTimeout = 2 * time.Second

	fake := &fakePool{}

	var remaining time.Duration

	_, err := openPool(
		context.Background(),
		Config{
			URL:            "postgres://localhost/platform",
			StartupTimeout: startupTimeout,
		},
		func(ctx context.Context, _ string) (*fakePool, error) {
			deadline, exists := ctx.Deadline()
			if !exists {
				t.Fatal("opener context does not have a deadline")
			}

			remaining = time.Until(deadline)

			return fake, nil
		},
	)
	if err != nil {
		t.Fatalf("openPool() returned error: %v", err)
	}

	if remaining <= 0 || remaining > startupTimeout {
		t.Fatalf(
			"remaining startup timeout = %v, want between 0 and %v",
			remaining,
			startupTimeout,
		)
	}
}

func TestOpenPoolRejectsEmptyURL(t *testing.T) {
	t.Parallel()

	called := false

	got, err := openPool(
		context.Background(),
		Config{},
		func(context.Context, string) (*fakePool, error) {
			called = true
			return &fakePool{}, nil
		},
	)

	if !errors.Is(err, ErrURLRequired) {
		t.Fatalf("openPool() error = %v, want ErrURLRequired", err)
	}

	if got != nil {
		t.Fatalf("openPool() returned pool %p, want nil", got)
	}

	if called {
		t.Fatal("opener was called for an empty database URL")
	}
}

func TestOpenPoolRejectsNegativeStartupTimeout(t *testing.T) {
	t.Parallel()

	called := false

	got, err := openPool(
		context.Background(),
		Config{
			URL:            "postgres://localhost/platform",
			StartupTimeout: -time.Second,
		},
		func(context.Context, string) (*fakePool, error) {
			called = true
			return &fakePool{}, nil
		},
	)

	if !errors.Is(err, ErrInvalidStartupTimeout) {
		t.Fatalf(
			"openPool() error = %v, want ErrInvalidStartupTimeout",
			err,
		)
	}

	if got != nil {
		t.Fatalf("openPool() returned pool %p, want nil", got)
	}

	if called {
		t.Fatal("opener was called with a negative startup timeout")
	}
}

func TestOpenPoolClosesPoolWhenPingFails(t *testing.T) {
	t.Parallel()

	driverErr := errors.New(
		"dial postgres://admin:private-password@localhost/platform",
	)

	fake := &fakePool{
		pingErr: driverErr,
	}

	got, err := openPool(
		context.Background(),
		Config{
			URL: "postgres://admin:private-password@localhost/platform",
		},
		func(context.Context, string) (*fakePool, error) {
			return fake, nil
		},
	)

	if !errors.Is(err, driverErr) {
		t.Fatalf("openPool() error = %v, want wrapped driver error", err)
	}

	if got != nil {
		t.Fatalf("openPool() returned pool %p, want nil", got)
	}

	if !fake.closed {
		t.Fatal("pool was not closed after Ping() failed")
	}

	if strings.Contains(err.Error(), "private-password") {
		t.Fatal("startup error leaked database credentials")
	}
}

func TestOpenPoolHidesConnectionStringWhenOpenFails(t *testing.T) {
	t.Parallel()

	const databaseURL = "postgres://admin:private-password@localhost/platform"

	driverErr := errors.New("cannot parse " + databaseURL)

	got, err := openPool(
		context.Background(),
		Config{
			URL: databaseURL,
		},
		func(context.Context, string) (*fakePool, error) {
			return nil, driverErr
		},
	)

	if !errors.Is(err, driverErr) {
		t.Fatalf("openPool() error = %v, want wrapped driver error", err)
	}

	if got != nil {
		t.Fatalf("openPool() returned pool %p, want nil", got)
	}

	if strings.Contains(err.Error(), "private-password") {
		t.Fatal("startup error leaked database credentials")
	}
}

func TestOpenMigratedClosesPoolWhenMigrationFails(t *testing.T) {
	driverErr := errors.New("migration failed")
	fake := &fakePool{}

	got, err := openMigrated(
		context.Background(),
		Config{URL: "postgres://localhost/platform"},
		func(context.Context, Config) (*fakePool, error) {
			return fake, nil
		},
		func(context.Context, *fakePool) error {
			return driverErr
		},
	)
	if !errors.Is(err, driverErr) || got != nil {
		t.Fatalf("openMigrated() = (%v, %v), want nil and wrapped migration error", got, err)
	}
	if !fake.closed {
		t.Fatal("pool was not closed after migration failed")
	}
}

func TestOpenMigratedReturnsReadyPool(t *testing.T) {
	fake := &fakePool{}
	migrated := false

	got, err := openMigrated(
		context.Background(),
		Config{URL: "postgres://localhost/platform"},
		func(context.Context, Config) (*fakePool, error) {
			return fake, nil
		},
		func(_ context.Context, pool *fakePool) error {
			migrated = pool == fake
			return nil
		},
	)
	if err != nil || got != fake || !migrated || fake.closed {
		t.Fatalf("openMigrated() did not return a migrated open pool: pool=%v error=%v migrated=%v closed=%v", got, err, migrated, fake.closed)
	}
}
