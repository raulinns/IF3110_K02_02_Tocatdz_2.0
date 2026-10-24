package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Meniru request setelah middleware: principal terpasang dan body form sudah diparse
func formRequest(method, target string, form url.Values) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r = r.WithContext(security.WithPrincipal(context.Background(), testPrincipal))
	if form != nil {
		r.PostForm = form
	}
	return r
}

func TestReadFormAcceptsOnlyExpectedSingleFields(t *testing.T) {
	r := formRequest("POST", "/x", url.Values{"title": {"a"}, "csrf_token": {"t"}})
	if values, err := readForm(r, "title", "sql"); err != nil || values.Get("title") != "a" {
		t.Fatal("valid form rejected")
	}
	for _, scenario := range []struct {
		name string
		r    *http.Request
	}{
		{"query string", formRequest("POST", "/x?title=b", url.Values{"title": {"a"}})},
		{"unparsed body", formRequest("POST", "/x", nil)},
		{"unknown field", formRequest("POST", "/x", url.Values{"title": {"a"}, "owner": {"x"}})},
		{"duplicate field", formRequest("POST", "/x", url.Values{"title": {"a", "b"}})},
	} {
		if _, err := readForm(scenario.r, "title", "sql"); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("form accepted: " + scenario.name)
		}
	}
}

func TestFormRevision(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "abc", "1.5", "18446744073709551616"} {
		if _, err := formRevision(url.Values{"revision": {value}}); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("invalid revision accepted: " + value)
		}
	}
	if revision, err := formRevision(url.Values{"revision": {"7"}}); err != nil || revision != 7 {
		t.Fatal("valid revision rejected")
	}
}

func TestFormTextTrimsAndBounds(t *testing.T) {
	if value, ok := formText(url.Values{"name": {"  tim data  "}}, "name", 10); !ok || value != "tim data" {
		t.Fatal("valid text rejected")
	}
	for _, value := range []string{"", "   ", "sebelas huruf"} {
		if _, ok := formText(url.Values{"name": {value}}, "name", 10); ok {
			t.Fatal("invalid text accepted: " + value)
		}
	}
}
