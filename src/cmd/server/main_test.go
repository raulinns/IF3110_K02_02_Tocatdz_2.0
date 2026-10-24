package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb"
)

type fakeDatabasePool struct {
	closed bool
}

func (p *fakeDatabasePool) Close() {
	p.closed = true
}

type fakeHTTPServer struct {
	started        chan struct{}
	stopped        chan struct{}
	listenCalled   bool
	shutdownCalled bool
}

func newFakeHTTPServer() *fakeHTTPServer {
	return &fakeHTTPServer{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (s *fakeHTTPServer) ListenAndServe() error {
	s.listenCalled = true
	close(s.started)

	<-s.stopped

	return http.ErrServerClosed
}

func (s *fakeHTTPServer) Shutdown(context.Context) error {
	s.shutdownCalled = true
	close(s.stopped)

	return nil
}

func TestRunApplicationUsesDatabaseAndClosesItAfterShutdown(
	t *testing.T,
) {
	const databaseURL = "postgres://app:secret@localhost:5433/platform"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	pool := &fakeDatabasePool{}
	server := newFakeHTTPServer()

	var gotDatabaseConfig platformdb.Config

	result := make(chan error, 1)

	go func() {
		result <- runApplication(
			ctx,
			config.Config{
				HTTPAddr:    ":8080",
				DatabaseURL: databaseURL,
			},
			logger,
			server,
			func(
				_ context.Context,
				cfg platformdb.Config,
			) (databasePool, error) {
				gotDatabaseConfig = cfg

				return pool, nil
			},
		)
	}()

	<-server.started

	cancel()

	err := <-result
	if err != nil {
		t.Fatalf("runApplication() returned error: %v", err)
	}

	if gotDatabaseConfig.URL != databaseURL {
		t.Fatalf(
			"database URL = %q, want %q",
			gotDatabaseConfig.URL,
			databaseURL,
		)
	}

	if !server.listenCalled {
		t.Fatal("HTTP server was not started")
	}

	if !server.shutdownCalled {
		t.Fatal("HTTP server was not shut down")
	}

	if !pool.closed {
		t.Fatal("database pool was not closed")
	}
}

func TestRunApplicationDoesNotStartServerWhenDatabaseOpenFails(
	t *testing.T,
) {
	driverErr := errors.New("database unavailable")

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	server := newFakeHTTPServer()

	err := runApplication(
		context.Background(),
		config.Config{
			HTTPAddr:    ":8080",
			DatabaseURL: "postgres://localhost/platform",
		},
		logger,
		server,
		func(
			context.Context,
			platformdb.Config,
		) (databasePool, error) {
			return nil, driverErr
		},
	)

	if !errors.Is(err, driverErr) {
		t.Fatalf(
			"runApplication() error = %v, want wrapped driver error",
			err,
		)
	}

	if server.listenCalled {
		t.Fatal("HTTP server started after database open failure")
	}
}
