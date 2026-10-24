package config

import (
	"bufio"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const defaultPublicOrigin = "http://localhost:8080"

var (
	ErrPublicOriginInvalid = errors.New("PUBLIC_ORIGIN must be an exact http(s) origin; production requires https")
	ErrKeyInvalid          = errors.New("SESSION_ENCRYPTION_KEY, CSRF_HMAC_KEY and CONNECTION_ENCRYPTION_KEY must be distinct base64 encodings of 32 random bytes")
)

// Security dipisah dari Config supaya nilai rahasia tidak ikut tercetak bersama konfigurasi biasa.
type Security struct {
	PublicOrigin  string
	Production    bool
	SessionKey    security.Secret
	CSRFKey       security.Secret
	ConnectionKey security.Secret
}

func validKey(value string) bool {
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	return err == nil && len(raw) == 32 && base64.StdEncoding.EncodeToString(raw) == value
}

func LoadSecurity(appEnv string) (Security, error) {
	production := appEnv == "production"
	origin := valueOrDefault(os.Getenv("PUBLIC_ORIGIN"), "")
	if origin == "" && !production {
		origin = defaultPublicOrigin
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil ||
		(parsed.Scheme != "https" && (production || parsed.Scheme != "http")) {
		return Security{}, ErrPublicOriginInvalid
	}
	keys := []string{
		strings.TrimSpace(os.Getenv("SESSION_ENCRYPTION_KEY")),
		strings.TrimSpace(os.Getenv("CSRF_HMAC_KEY")),
		strings.TrimSpace(os.Getenv("CONNECTION_ENCRYPTION_KEY")),
	}
	for i, key := range keys {
		if !validKey(key) {
			return Security{}, ErrKeyInvalid
		}
		for _, other := range keys[:i] {
			if key == other {
				return Security{}, ErrKeyInvalid
			}
		}
	}
	return Security{
		PublicOrigin: origin, Production: production,
		SessionKey: security.NewSecret(keys[0]), CSRFKey: security.NewSecret(keys[1]), ConnectionKey: security.NewSecret(keys[2]),
	}, nil
}

// LoadDotEnv mengisi variabel dari file .env untuk pengembangan lokal tanpa menimpa variabel yang sudah diset.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
