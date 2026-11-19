package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const requestIDHeader = "X-Request-ID"

const contentSecurityPolicy = security.ContentSecurityPolicy

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
				requestMethod(r),
				"route",
				requestRoute(r),
			)

			w.Header().Set("Cache-Control", "no-store")
			http.Error(
				w,
				http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError,
			)

			return
		}

		ctx := security.WithRequestID(r.Context(), requestID)

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
			requestMethod(r),
			"route",
			requestRoute(r),
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
			failure := recover()
			if failure == nil {
				return
			}
			if failure == http.ErrAbortHandler {
				panic(failure)
			}

			logger.ErrorContext(
				r.Context(),
				"panic recovered",
				"request_id",
				requestIDFromContext(r.Context()),
				"method",
				requestMethod(r),
				"route",
				requestRoute(r),
			)

			w.Header().Set("Cache-Control", "no-store")
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
	return security.SecurityHeaders(next)
}

func generateRequestID() (string, error) {
	var value [16]byte

	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(value[:]), nil
}

// Catat pola route agar nilai URL dari client tidak ikut masuk log.
func requestRoute(r *http.Request) string {
	if r.Pattern == "" {
		return "[unmatched]"
	}
	return r.Pattern
}

func requestMethod(r *http.Request) string {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return r.Method
	default:
		return "[other]"
	}
}

func requestIDFromContext(ctx context.Context) string {
	return security.RequestIDFromContext(ctx)
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
