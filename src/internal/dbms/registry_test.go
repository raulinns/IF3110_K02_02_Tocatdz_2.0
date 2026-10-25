package dbms

import (
	"errors"
	"slices"
	"testing"
)

type stubAdapter struct {
	engine Engine
}

func (a *stubAdapter) Engine() Engine {
	return a.engine
}

func TestRegistryResolvesAdapterByEngine(t *testing.T) {
	t.Parallel()

	postgres := &stubAdapter{engine: PostgreSQL}

	registry, err := NewRegistry(postgres)
	if err != nil {
		t.Fatalf("NewRegistry() returned error: %v", err)
	}

	tests := []struct {
		name   string
		engine Engine
		want   *stubAdapter
	}{
		{
			name:   "PostgreSQL",
			engine: PostgreSQL,
			want:   postgres,
		},
		{
			name:   "PostgreSQL alias",
			engine: Engine("postgres"),
			want:   postgres,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := registry.Resolve(tt.engine)
			if err != nil {
				t.Fatalf(
					"Resolve(%q) returned error: %v",
					tt.engine,
					err,
				)
			}

			if got != tt.want {
				t.Fatalf(
					"Resolve(%q) returned adapter %p, want %p",
					tt.engine,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestRegistryReturnsRegisteredEnginesInStableOrder(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(
		&stubAdapter{engine: PostgreSQL},
	)
	if err != nil {
		t.Fatalf("NewRegistry() returned error: %v", err)
	}

	got := registry.Engines()
	want := []Engine{PostgreSQL}

	if !slices.Equal(got, want) {
		t.Fatalf("Engines() = %v, want %v", got, want)
	}
}

func TestRegistryRejectsDuplicateEngine(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(
		&stubAdapter{engine: PostgreSQL},
		&stubAdapter{engine: Engine("postgres")},
	)

	if !errors.Is(err, ErrDuplicateAdapter) {
		t.Fatalf(
			"NewRegistry() error = %v, want ErrDuplicateAdapter",
			err,
		)
	}
}

func TestRegistryRejectsNilAdapter(t *testing.T) {
	t.Parallel()

	var adapter *stubAdapter

	_, err := NewRegistry(adapter)

	if !errors.Is(err, ErrInvalidAdapter) {
		t.Fatalf(
			"NewRegistry() error = %v, want ErrInvalidAdapter",
			err,
		)
	}
}

func TestRegistryRejectsUnsupportedEngine(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(
		&stubAdapter{engine: Engine("mariadb")},
	)

	if !errors.Is(err, ErrUnsupportedEngine) {
		t.Fatalf(
			"NewRegistry() error = %v, want ErrUnsupportedEngine",
			err,
		)
	}
}

func TestRegistryReturnsNotFoundForUnregisteredEngine(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry[*stubAdapter]()
	if err != nil {
		t.Fatalf("NewRegistry() returned error: %v", err)
	}

	got, err := registry.Resolve(PostgreSQL)

	if !errors.Is(err, ErrAdapterNotFound) {
		t.Fatalf(
			"Resolve(%q) error = %v, want ErrAdapterNotFound",
			PostgreSQL,
			err,
		)
	}

	if got != nil {
		t.Fatalf(
			"Resolve(%q) returned adapter %p, want nil",
			PostgreSQL,
			got,
		)
	}
}

func TestRegistryRejectsUnsupportedEngineDuringResolve(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(
		&stubAdapter{engine: PostgreSQL},
	)
	if err != nil {
		t.Fatalf("NewRegistry() returned error: %v", err)
	}

	got, err := registry.Resolve(Engine("sqlite"))

	if !errors.Is(err, ErrUnsupportedEngine) {
		t.Fatalf(
			"Resolve(%q) error = %v, want ErrUnsupportedEngine",
			"sqlite",
			err,
		)
	}

	if got != nil {
		t.Fatalf(
			"Resolve(%q) returned adapter %p, want nil",
			"sqlite",
			got,
		)
	}
}
