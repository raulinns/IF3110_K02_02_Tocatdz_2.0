package security

import (
	"net/http"
	"strings"
)

type SessionCookies struct {
	secure     bool
	configured bool
}

func NewSessionCookies(publicOrigin string, production bool) (*SessionCookies, error) {
	origin, err := parseOrigin(publicOrigin, false)
	if err != nil || production && origin.scheme != "https" {
		return nil, ErrInvalidInput
	}
	return &SessionCookies{secure: origin.scheme == "https", configured: true}, nil
}

func (c *SessionCookies) Read(r *http.Request) (Secret, error) {
	if c == nil || !c.configured || r == nil {
		return Secret{}, ErrInvalidInput
	}
	var value string
	count := 0
	for _, line := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(line, ";") {
			name, candidate, hasEquals := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(name) != SessionCookieName {
				continue
			}
			count++
			if !hasEquals || name != SessionCookieName {
				return Secret{}, ErrUnauthenticated
			}
			value = candidate
		}
	}
	if count != 1 {
		return Secret{}, ErrUnauthenticated
	}
	token := NewSecret(value)
	if _, err := decodeToken(token); err != nil {
		return Secret{}, ErrUnauthenticated
	}
	return token, nil
}

func (c *SessionCookies) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: SessionCookieName, Value: value, Path: "/", HttpOnly: true,
		Secure: c.secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	}
}

// Kirim cookie setelah session berhasil disimpan, sebelum menulis body response
func (c *SessionCookies) Write(w http.ResponseWriter, token Secret) error {
	if c == nil || !c.configured || w == nil {
		return ErrInvalidInput
	}
	if _, err := decodeToken(token); err != nil {
		return ErrInvalidInput
	}
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, c.cookie(token.Reveal(), 0))
	return nil
}

func (c *SessionCookies) Clear(w http.ResponseWriter) error {
	if c == nil || !c.configured || w == nil {
		return ErrInvalidInput
	}
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, c.cookie("", -1))
	return nil
}
