package dbms

import (
	"errors"
	"reflect"
	"sort"
)

var (
	ErrInvalidAdapter   = errors.New("invalid DBMS adapter")
	ErrDuplicateAdapter = errors.New("duplicate DBMS adapter")
	ErrAdapterNotFound  = errors.New("DBMS adapter not found")
)

type Adapter interface {
	Engine() Engine
}

type Registry[T Adapter] struct {
	adapters map[Engine]T
}

func NewRegistry[T Adapter](adapters ...T) (*Registry[T], error) {
	registry := &Registry[T]{
		adapters: make(map[Engine]T, len(adapters)),
	}

	for _, adapter := range adapters {
		if isNilAdapter(adapter) {
			return nil, ErrInvalidAdapter
		}

		engine, err := ParseEngine(string(adapter.Engine()))
		if err != nil {
			return nil, err
		}

		if _, exists := registry.adapters[engine]; exists {
			return nil, ErrDuplicateAdapter
		}

		registry.adapters[engine] = adapter
	}

	return registry, nil
}

func (r *Registry[T]) Resolve(engine Engine) (T, error) {
	var zero T

	normalized, err := ParseEngine(string(engine))
	if err != nil {
		return zero, err
	}

	adapter, exists := r.adapters[normalized]
	if !exists {
		return zero, ErrAdapterNotFound
	}

	return adapter, nil
}

func (r *Registry[T]) Engines() []Engine {
	engines := make([]Engine, 0, len(r.adapters))

	for engine := range r.adapters {
		engines = append(engines, engine)
	}

	sort.Slice(engines, func(i, j int) bool {
		return engines[i] < engines[j]
	})

	return engines
}

func isNilAdapter[T Adapter](adapter T) bool {
	value := reflect.ValueOf(adapter)

	switch value.Kind() {
	case reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice:
		return value.IsNil()

	default:
		return false
	}
}
