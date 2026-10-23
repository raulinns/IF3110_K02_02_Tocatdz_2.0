package security_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestSecretDoesNotLeakThroughFormatting(t *testing.T) {
	const canary = "sensitive-canary-password-or-session-token"
	secret := security.NewSecret(canary)
	if secret.Empty() || secret.Reveal() != canary || !(security.Secret{}).Empty() {
		t.Fatal("secret crypto-boundary accessor does not preserve value")
	}
	for _, format := range []string{"%s", "%q", "%v", "%+v", "%#v", "%x"} {
		for _, value := range []any{secret, &secret, struct{ Password security.Secret }{secret}} {
			formatted := fmt.Sprintf(format, value)
			if strings.Contains(formatted, canary) {
				t.Fatalf("secret leaked through %s", format)
			}
		}
	}
}

func TestSecretCannotBeSerialized(t *testing.T) {
	secret := security.NewSecret("never-serialize")
	for _, value := range []any{secret, struct{ Token security.Secret }{secret}} {
		if _, err := json.Marshal(value); !errors.Is(err, security.ErrSecretSerialization) {
			t.Errorf("JSON must fail closed: %v", err)
		}
	}
	if _, err := secret.MarshalText(); !errors.Is(err, security.ErrSecretSerialization) {
		t.Fatal("text serialization must fail closed")
	}
}

func TestSecretSlogRedaction(t *testing.T) {
	const canary = "sensitive-log-canary"
	for _, jsonHandler := range []bool{false, true} {
		var output bytes.Buffer
		var handler slog.Handler = slog.NewTextHandler(&output, nil)
		if jsonHandler {
			handler = slog.NewJSONHandler(&output, nil)
		}
		slog.New(handler).Info("test", "secret", security.NewSecret(canary))
		if strings.Contains(output.String(), canary) || !strings.Contains(output.String(), "REDACTED") {
			t.Fatal("slog did not redact the secret")
		}
	}
}
