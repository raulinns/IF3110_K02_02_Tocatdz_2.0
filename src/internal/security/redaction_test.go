package security

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretInsidePrivateFieldsDoesNotLeak(t *testing.T) {
	const canary = "private-CSRF-password-key-canary"
	secret := NewSecret(canary)
	private := struct{ token Secret }{secret}
	nested := struct{ wrapped any }{private}
	values := []any{
		secret, &secret, private, &private, nested, &nested,
		requestView{csrf: secret}, &requestView{csrf: secret},
		AuthService{dummyHash: secret}, &AuthService{dummyHash: secret},
		HMACCSRFGuard{key: secret}, &HMACCSRFGuard{key: secret},
	}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
			got := fmt.Sprintf(format, value)
			for _, leaked := range []string{canary, hex.EncodeToString([]byte(canary)), strings.ToUpper(hex.EncodeToString([]byte(canary))), base64.StdEncoding.EncodeToString([]byte(canary))} {
				if strings.Contains(got, leaked) {
					t.Fatalf("private secret leaked through %s on %T", format, value)
				}
			}
		}
		for _, jsonLog := range []bool{false, true} {
			var output bytes.Buffer
			var handler slog.Handler = slog.NewTextHandler(&output, nil)
			if jsonLog {
				handler = slog.NewJSONHandler(&output, nil)
			}
			slog.New(handler).Info("test", "value", value)
			if strings.Contains(output.String(), canary) {
				t.Fatalf("slog leaked private secret on %T", value)
			}
		}
	}
}

func TestRequestContextRedactsCSRFWithoutChangingAccessors(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	view := requestView{identity: result.Identity, csrf: result.CSRFToken}
	for _, value := range []any{view, &view, *f.guard, f.guard, *f.service, f.service} {
		formatted := fmt.Sprintf("%+v", value)
		if formatted != "[REDACTED]" {
			t.Fatalf("sensitive wrapper %T is not redacted", value)
		}
	}
	encoded, err := json.Marshal(view)
	if err != nil || strings.Contains(string(encoded), result.CSRFToken.Reveal()) {
		t.Fatal("request view serialized CSRF")
	}
	if NewSecret("").Reveal() != "" || !(Secret{}).Empty() || !NewSecret("").Empty() {
		t.Fatal("zero/empty Secret changed behavior")
	}
}

func TestSessionMetadataRedactsFormattingAndLogs(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	snapshot, err := f.store.FindByID(t.Context(), result.Principal.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	values := []any{result.Principal, &result.Principal, snapshot.Session, &snapshot.Session, snapshot, &snapshot, result, &result, snapshot.Session.CurrentDigest, Credentials{Username: "CREDENTIAL_PRIVATE_CANARY", Password: NewSecret("password")}}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%x", "%q"} {
			if got := fmt.Sprintf(format, value); got != "[REDACTED]" {
				t.Fatal("sensitive session value was not redacted")
			}
		}
		for _, jsonLog := range []bool{false, true} {
			var output bytes.Buffer
			var handler slog.Handler = slog.NewTextHandler(&output, nil)
			if jsonLog {
				handler = slog.NewJSONHandler(&output, nil)
			}
			slog.New(handler).Info("audit", "value", value)
			for _, secret := range []string{string(result.Principal.SessionID), result.CookieToken.Reveal(), result.CSRFToken.Reveal(), "CREDENTIAL_PRIVATE_CANARY"} {
				if strings.Contains(output.String(), secret) {
					t.Fatal("session log leaked metadata")
				}
			}
		}
	}
}
