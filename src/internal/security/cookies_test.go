package security

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCookieAttributesAndDeletion(t *testing.T) {
	for _, public := range []string{"http://localhost:8080", "https://platform.example"} {
		cookies, err := NewSessionCookies(public, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, deleting := range []bool{false, true} {
			w := httptest.NewRecorder()
			if deleting {
				err = cookies.Clear(w)
			} else {
				err = cookies.Write(w, testToken(1))
			}
			if err != nil {
				t.Fatal(err)
			}
			got := w.Result().Cookies()
			if len(got) != 1 {
				t.Fatal("missing/duplicate Set-Cookie")
			}
			c := got[0]
			if c.Name != SessionCookieName || c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" || !c.Expires.IsZero() || c.Secure != strings.HasPrefix(public, "https:") {
				t.Fatal("incorrect cookie security attributes")
			}
			if deleting && (c.Value != "" || c.MaxAge != -1) || !deleting && (c.Value != testToken(1).Reveal() || c.MaxAge != 0) {
				t.Fatal("wrong browser-session/deletion lifetime")
			}
			if w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cookie leaked into body or response may be cached")
			}
		}
	}
}

func TestCookieReaderRejectsAmbiguityAndNonCookieTransport(t *testing.T) {
	cookies, _ := NewSessionCookies("https://platform.example", true)
	valid := SessionCookieName + "=" + testToken(1).Reveal()
	for _, lines := range [][]string{
		nil, {valid + "; " + valid}, {valid, valid}, {valid, SessionCookieName + "="},
		{valid + "; " + SessionCookieName}, {SessionCookieName + "=bad"},
		{SessionCookieName + "=\"" + testToken(1).Reveal() + "\""},
		{valid + "; " + SessionCookieName + " =" + testToken(2).Reveal()},
	} {
		r := httptest.NewRequest("GET", "/?"+SessionCookieName+"="+testToken(1).Reveal(), nil)
		r.Header.Set("Authorization", "Bearer "+testToken(1).Reveal())
		for _, line := range lines {
			r.Header.Add("Cookie", line)
		}
		if token, err := cookies.Read(r); !errors.Is(err, ErrUnauthenticated) || !token.Empty() {
			t.Fatal("missing/malformed/ambiguous cookie accepted")
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cookie", "other=value; "+valid)
	if token, err := cookies.Read(r); err != nil || token.Reveal() != testToken(1).Reveal() {
		t.Fatal("valid cookie rejected")
	}
}

func TestCookieConfigurationFailsClosed(t *testing.T) {
	for _, origin := range []string{"", "http://platform.example", "https://user:password@platform.example", "https://platform.example/path", "https://platform.example?token=x", "https://platform.example#fragment"} {
		if _, err := NewSessionCookies(origin, true); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid production origin accepted")
		}
	}
	cookies, _ := NewSessionCookies("https://platform.example", true)
	w := httptest.NewRecorder()
	if err := cookies.Write(w, NewSecret("bad")); !errors.Is(err, ErrInvalidInput) || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("invalid token emitted")
	}
	var zero SessionCookies
	if err := zero.Clear(w); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unconfigured cookie helper accepted")
	}
}
