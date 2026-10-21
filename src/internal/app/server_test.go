package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeHTTPServer struct {
	started           chan struct{}
	stopServing       chan struct{}
	shutdownCalled    chan struct{}
	serveErr          error
	shutdownErr       error
	returnImmediately bool
}

func newFakeHTTPServer() *fakeHTTPServer {
	return &fakeHTTPServer{
		started:        make(chan struct{}),
		stopServing:    make(chan struct{}),
		shutdownCalled: make(chan struct{}),
		serveErr:       http.ErrServerClosed,
	}
}

func (s *fakeHTTPServer) ListenAndServe() error {
	close(s.started)

	if s.returnImmediately {
		return s.serveErr
	}

	<-s.stopServing

	return s.serveErr
}

func (s *fakeHTTPServer) Shutdown(_ context.Context) error {
	close(s.shutdownCalled)
	close(s.stopServing)

	return s.shutdownErr
}

func TestRun(t *testing.T) {
	t.Run("shuts down server when context is canceled", func(t *testing.T) {
		server := newFakeHTTPServer()

		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)

		go func() {
			result <- Run(ctx, server)
		}()

		waitForSignal(t, server.started)
		cancel()

		waitForSignal(t, server.shutdownCalled)

		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Run did not return after server shutdown")
		}
	})

	t.Run("returns unexpected serving error", func(t *testing.T) {
		serveErr := errors.New("listen failed")

		server := newFakeHTTPServer()
		server.serveErr = serveErr
		server.returnImmediately = true

		err := Run(context.Background(), server)

		if !errors.Is(err, serveErr) {
			t.Fatalf("expected error %v, got %v", serveErr, err)
		}

		if !strings.Contains(err.Error(), "serve HTTP") {
			t.Errorf("expected serving context in error, got %q", err)
		}
	})

	t.Run("returns shutdown error", func(t *testing.T) {
		shutdownErr := errors.New("shutdown failed")

		server := newFakeHTTPServer()
		server.shutdownErr = shutdownErr

		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)

		go func() {
			result <- Run(ctx, server)
		}()

		waitForSignal(t, server.started)
		cancel()

		select {
		case err := <-result:
			if !errors.Is(err, shutdownErr) {
				t.Fatalf(
					"expected error %v, got %v",
					shutdownErr,
					err,
				)
			}

			if !strings.Contains(err.Error(), "shutdown HTTP server") {
				t.Errorf(
					"expected shutdown context in error, got %q",
					err,
				)
			}
		case <-time.After(time.Second):
			t.Fatal("Run did not return after shutdown failure")
		}
	})
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}
