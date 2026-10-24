package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func key(fill byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)) }

func setKeys(t *testing.T, session, csrf, connection string) {
	t.Setenv("SESSION_ENCRYPTION_KEY", session)
	t.Setenv("CSRF_HMAC_KEY", csrf)
	t.Setenv("CONNECTION_ENCRYPTION_KEY", connection)
}

func TestLoadSecurityDefaultsAndRedaction(t *testing.T) {
	setKeys(t, key(1), key(2), key(3))
	t.Setenv("PUBLIC_ORIGIN", "")
	got, err := LoadSecurity("development")
	if err != nil || got.PublicOrigin != "http://localhost:8080" || got.Production || got.SessionKey.Reveal() != key(1) {
		t.Fatal("development defaults not applied")
	}
	if strings.Contains(fmt.Sprintf("%v %+v", got, got), key(1)) {
		t.Fatal("key leaked through formatting")
	}
}

func TestLoadSecurityRejectsUnsafeConfiguration(t *testing.T) {
	for _, scenario := range []struct {
		name, env, origin, session, csrf, connection string
		want                                         error
	}{
		{"production http origin", "production", "http://platform.example", key(1), key(2), key(3), ErrPublicOriginInvalid},
		{"production missing origin", "production", "", key(1), key(2), key(3), ErrPublicOriginInvalid},
		{"origin with path", "development", "http://localhost:8080/app", key(1), key(2), key(3), ErrPublicOriginInvalid},
		{"origin with credentials", "development", "http://u:p@localhost:8080", key(1), key(2), key(3), ErrPublicOriginInvalid},
		{"missing key", "development", "", "", key(2), key(3), ErrKeyInvalid},
		{"short key", "development", "", base64.StdEncoding.EncodeToString([]byte("short")), key(2), key(3), ErrKeyInvalid},
		{"reused key", "development", "", key(1), key(1), key(3), ErrKeyInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("PUBLIC_ORIGIN", scenario.origin)
			setKeys(t, scenario.session, scenario.csrf, scenario.connection)
			if _, err := LoadSecurity(scenario.env); !errors.Is(err, scenario.want) {
				t.Fatalf("got %v want %v", err, scenario.want)
			}
		})
	}
	t.Setenv("PUBLIC_ORIGIN", "https://platform.example")
	setKeys(t, key(1), key(2), key(3))
	if got, err := LoadSecurity("production"); err != nil || !got.Production {
		t.Fatal("valid production configuration rejected")
	}
}

func TestLoadDotEnvDoesNotOverrideEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# komentar\nTOCATDZ_TEST_A=dari-file\nTOCATDZ_TEST_B=\"berkutip\"\nINVALID LINE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOCATDZ_TEST_A", "dari-env")
	os.Unsetenv("TOCATDZ_TEST_B")
	t.Cleanup(func() { os.Unsetenv("TOCATDZ_TEST_B") })
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TOCATDZ_TEST_A") != "dari-env" || os.Getenv("TOCATDZ_TEST_B") != "berkutip" {
		t.Fatal("dotenv precedence or quoting wrong")
	}
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatal("missing dotenv file should be ignored")
	}
}
