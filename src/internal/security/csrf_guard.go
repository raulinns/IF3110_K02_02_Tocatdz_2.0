package security

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"io"
	"mime"
	"net/http"
	"net/url"
)

const MaxCSRFFormBytes int64 = 2 << 20

type HMACCSRFGuard struct {
	publicOrigin origin
	key          Secret
}

var _ CSRFGuard = (*HMACCSRFGuard)(nil)

func NewHMACCSRFGuard(publicOrigin string, key Secret) (*HMACCSRFGuard, error) {
	configured, err := parseOrigin(publicOrigin, false)
	if err != nil {
		return nil, err
	}
	raw, err := decodeKey(key)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	return &HMACCSRFGuard{publicOrigin: configured, key: NewSecret(string(raw))}, nil
}

func SafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func (g *HMACCSRFGuard) VerifyOrigin(r *http.Request) error {
	if g == nil || g.key.Empty() || r == nil {
		return ErrInvalidInput
	}
	if SafeMethod(r.Method) {
		return nil
	}
	metadata := r.Header.Values("Sec-Fetch-Site")
	if len(metadata) > 1 || len(metadata) == 1 && metadata[0] != "same-origin" && metadata[0] != "same-site" && metadata[0] != "none" {
		return ErrCSRF
	}
	values := r.Header.Values("Origin")
	allowPath := false
	if len(values) == 0 {
		values = r.Header.Values("Referer")
		allowPath = true
	}
	if len(values) != 1 {
		return ErrCSRF
	}
	candidate, err := parseOrigin(values[0], allowPath)
	if err != nil || candidate != g.publicOrigin {
		return ErrCSRF
	}
	return nil
}

func (g *HMACCSRFGuard) Token(ctx context.Context, principal Principal) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	if g == nil || g.key.Empty() || !ValidID(principal.UserID) || !ValidID(principal.SessionID) || principal.AuthVersion == 0 || principal.CSRFEpoch == 0 {
		return Secret{}, ErrInvalidInput
	}
	mac := hmac.New(sha256.New, []byte(g.key.Reveal()))
	_, _ = mac.Write([]byte("tocatdz/csrf/v1\x00"))
	message := binary.BigEndian.AppendUint32(nil, uint32(len(principal.SessionID)))
	message = append(message, []byte(principal.SessionID)...)
	message = binary.BigEndian.AppendUint64(message, principal.CSRFEpoch)
	_, _ = mac.Write(message)
	return NewSecret(base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), nil
}

func (g *HMACCSRFGuard) VerifyToken(ctx context.Context, principal Principal, token Secret) error {
	expected, err := g.Token(ctx, principal)
	if err != nil {
		return err
	}
	if _, err := decodeToken(token); err != nil {
		return ErrCSRF
	}
	if subtle.ConstantTimeCompare([]byte(expected.Reveal()), []byte(token.Reveal())) != 1 {
		return ErrCSRF
	}
	return nil
}

// Baca token CSRF sebelum body diproses bagian lain
func ReadCSRFToken(w http.ResponseWriter, r *http.Request) (Secret, error) {
	if r == nil || r.URL == nil || SafeMethod(r.Method) {
		return Secret{}, ErrInvalidInput
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || query.Has(CSRFFieldName) {
		return Secret{}, ErrCSRF
	}
	headers := r.Header.Values(CSRFHeaderName)
	if len(headers) > 1 {
		return Secret{}, ErrCSRF
	}
	var fields []string
	if values := r.Header.Values("Content-Type"); len(values) > 1 {
		return Secret{}, ErrCSRF
	}
	contentType := r.Header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType == "multipart/form-data" {
			return Secret{}, ErrCSRF
		}
		if mediaType == "application/x-www-form-urlencoded" {
			if r.PostForm != nil || r.Body == nil {
				return Secret{}, ErrCSRF
			}
			r.Body = http.MaxBytesReader(w, r.Body, MaxCSRFFormBytes)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return Secret{}, ErrCSRF
			}
			// Parsing manual ini mendukung DELETE dan metode lain tanpa mencampur token dari query URL.
			r.PostForm, err = url.ParseQuery(string(body))
			if err != nil {
				return Secret{}, ErrCSRF
			}
			fields = r.PostForm[CSRFFieldName]
		}
	}
	if len(fields)+len(headers) != 1 {
		return Secret{}, ErrCSRF
	}
	value := ""
	if len(fields) == 1 {
		value = fields[0]
	} else {
		value = headers[0]
	}
	token := NewSecret(value)
	if _, err := decodeToken(token); err != nil {
		return Secret{}, ErrCSRF
	}
	return token, nil
}
