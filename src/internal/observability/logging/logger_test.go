package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestLoggerRedactsCredentialsAndUntrustedCorrelation(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output).With("trace_id", "secret-trace").With("component", "test")
	ctx := security.WithRequestID(context.Background(), "trusted-request")
	logger.InfoContext(ctx, "test event", "password", "secret-password", "dsn", "secret-dsn",
		"session_id", "secret-session", "error", errors.New("secret-driver"),
		"params", []string{"secret-param"})
	logger.WithGroup("credentials").With("account", "secret-account").InfoContext(ctx, "test grouping", "api_key", "secret-key")
	if strings.Contains(output.String(), "secret-") {
		t.Fatal("sensitive value escaped the redaction handler")
	}
	decoder := json.NewDecoder(&output)
	for i := 0; i < 2; i++ {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event["request_id"] != "trusted-request" || event["trace_id"] != nil || event["level"] != "INFO" {
			t.Fatalf("unexpected correlation or level: %v", event)
		}
	}
}
