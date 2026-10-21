package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

const requestIDHeader = "X-Request-ID"

const contentSecurityPolicy = "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'"

type requestIDContextKey struct{}

type responseRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func middlewareStack(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	return securityHeaderMiddleware(
		requestIDMiddleware(
			logger,
			requestLoggingMiddleware(
				logger,
				recoveryMiddleware(logger, next),
			),
		),
	)
}

func requestIDMiddleware(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, err := generateRequestID()
		if err != nil {
			logger.ErrorContext(
				r.Context(),
				"failed to generate request ID",
				"method",
				r.Method,
				"path",
				r.URL.Path,
			)

			http.Error(
				w,
				http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError,
			)

			return
		}

		ctx := context.WithValue(
			r.Context(),
			requestIDContextKey{},
			requestID,
		)

		w.Header().Set(requestIDHeader, requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestLoggingMiddleware(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()

		recorder := &responseRecorder{
			ResponseWriter: w,
		}

		next.ServeHTTP(recorder, r)

		statusCode := recorder.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		logger.InfoContext(
			r.Context(),
			"HTTP request",
			"request_id",
			requestIDFromContext(r.Context()),
			"method",
			r.Method,
			"path",
			r.URL.Path,
			"status",
			statusCode,
			"bytes",
			recorder.bytesWritten,
			"duration_ms",
			time.Since(startedAt).Milliseconds(),
		)
	})
}

func recoveryMiddleware(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() == nil {
				return
			}

			logger.ErrorContext(
				r.Context(),
				"panic recovered",
				"request_id",
				requestIDFromContext(r.Context()),
				"method",
				r.Method,
				"path",
				r.URL.Path,
			)

			http.Error(
				w,
				http.StatusText(http.StatusInternalServerError),
				http.StatusInternalServerError,
			)
		}()

		next.ServeHTTP(w, r)
	})
}

func securityHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()

		headers.Set(
			"Content-Security-Policy",
			contentSecurityPolicy,
		)

		headers.Set(
			"Referrer-Policy",
			"strict-origin-when-cross-origin",
		)

		headers.Set(
			"X-Content-Type-Options",
			"nosniff",
		)
		headers.Set(
			"X-Frame-Options",
			"DENY",
		)

		headers.Set(
			"Permissions-Policy",
			"camera=(), microphone=(), geolocation=()",
		)
		headers.Set(
			"Cross-Origin-Opener-Policy",
			"same-origin",
		)
		headers.Set(
			"Cross-Origin-Resource-Policy",
			"same-origin",
		)

		next.ServeHTTP(w, r)
	})
}

func generateRequestID() (string, error) {
	var value [16]byte

	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(value[:]), nil
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)

	return requestID
}

func (w *responseRecorder) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}

	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseRecorder) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}

	written, err := w.ResponseWriter.Write(data)
	w.bytesWritten += written

	return written, err
}

func (w *responseRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
