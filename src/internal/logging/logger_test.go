package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	t.Run("uses JSON logging in production", func(t *testing.T) {
		var output bytes.Buffer

		logger := New("production", &output)
		logger.Info("application started", "component", "test")

		var record map[string]any

		if err := json.Unmarshal(output.Bytes(), &record); err != nil {
			t.Fatalf("expected valid JSON log, got %q: %v", output.String(), err)
		}

		if record["level"] != "INFO" {
			t.Errorf("expected INFO level, got %v", record["level"])
		}

		if record["msg"] != "application started" {
			t.Errorf(
				"expected message %q, got %v",
				"application started",
				record["msg"],
			)
		}

		if record["component"] != "test" {
			t.Errorf("expected component test, got %v", record["component"])
		}
	})

	t.Run("uses text logging outside production", func(t *testing.T) {
		var output bytes.Buffer

		logger := New("development", &output)
		logger.Info("application started", "component", "test")

		logLine := output.String()

		if !strings.Contains(logLine, "level=INFO") {
			t.Errorf("expected INFO level, got %q", logLine)
		}

		if !strings.Contains(logLine, `msg="application started"`) {
			t.Errorf("expected application message, got %q", logLine)
		}

		if !strings.Contains(logLine, "component=test") {
			t.Errorf("expected component attribute, got %q", logLine)
		}
	})
}
