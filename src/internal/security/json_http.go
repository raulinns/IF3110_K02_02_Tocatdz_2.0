package security

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// JSONMediaType deliberately accepts only application/json, optionally UTF-8.
// It does not read/parse the body or accept +json/form media types implicitly.
func JSONMediaType(r *http.Request) bool {
	if r == nil || len(r.Header.Values("Content-Type")) != 1 {
		return false
	}
	kind, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		return false
	}
	for name, value := range params {
		if name != "charset" || !strings.EqualFold(value, "utf-8") {
			return false
		}
	}
	return true
}

func (m *HTTPMiddleware) checkLoginRequest(r *http.Request) error {
	if m.deps.Transport == HTMLTransport {
		if r.URL.RawQuery != "" || !loginFormType(r) || r.ContentLength > MaxLoginFormBytes || r.PostForm != nil || r.Body == nil {
			return ErrInvalidInput
		}
		return nil
	}
	if r.URL.RawQuery != "" || r.PostForm != nil || r.Body == nil {
		return ErrMalformedRequest
	}
	if !JSONMediaType(r) {
		return ErrUnsupportedMediaType
	}
	if r.ContentLength > MaxLoginFormBytes {
		return ErrPayloadTooLarge
	}
	return nil
}

func (m *HTTPMiddleware) readCSRFToken(w http.ResponseWriter, r *http.Request) (Secret, error) {
	if m.deps.Transport == JSONTransport {
		return ReadCSRFHeaderToken(r)
	}
	return ReadCSRFToken(w, r)
}

// ReadCSRFHeaderToken leaves JSON bytes untouched. Tokens in query strings or
// form bodies never substitute for the required API header.
func ReadCSRFHeaderToken(r *http.Request) (Secret, error) {
	if r == nil || r.URL == nil || SafeMethod(r.Method) {
		return Secret{}, ErrCSRF
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || query.Has(CSRFFieldName) {
		return Secret{}, ErrCSRF
	}
	values := r.Header.Values(CSRFHeaderName)
	if len(values) != 1 {
		return Secret{}, ErrCSRF
	}
	token := NewSecret(values[0])
	if _, err := decodeToken(token); err != nil {
		return Secret{}, ErrCSRF
	}
	return token, nil
}
