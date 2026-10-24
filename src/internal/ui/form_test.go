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
func protectedRequest(method, target string, form url.Values) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r = r.WithContext(security.WithPrincipal(context.Background(), testPrincipal))
	if form != nil {
		r.PostForm = form
	}
	return r
}

func formRequest(method, target string, form url.Values) *http.Request {
	return protectedRequest(method, target, form)
}

func TestReadFormAcceptsOnlyExpectedSingleFields(t *testing.T) {
	r := protectedRequest("POST", "/x", url.Values{"title": {"a"}, "csrf_token": {"t"}})
	if values, err := readForm(r, "title", "sql"); err != nil || values.Get("title") != "a" {
		t.Fatal("valid form rejected")
	}
	for _, scenario := range []struct {
		name string
		r    *http.Request
	}{
		{"query string", protectedRequest("POST", "/x?title=b", url.Values{"title": {"a"}})},
		{"unparsed body", protectedRequest("POST", "/x", nil)},
		{"unknown field", protectedRequest("POST", "/x", url.Values{"title": {"a"}, "owner": {"x"}})},
		{"duplicate field", protectedRequest("POST", "/x", url.Values{"title": {"a", "b"}})},
	} {
		if _, err := readForm(scenario.r, "title", "sql"); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("form accepted: " + scenario.name)
		}
	}
}

func TestFormRevisionAndBoundedQuery(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "abc", "1.5", "18446744073709551616"} {
		if _, err := formRevision(url.Values{"revision": {value}}); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("invalid revision accepted: " + value)
		}
	}
	if revision, err := formRevision(url.Values{"revision": {"7"}}); err != nil || revision != 7 {
		t.Fatal("valid revision rejected")
	}
	if n, err := boundedQueryInt(url.Values{}, "limit", 50, 1, 200); err != nil || n != 50 {
		t.Fatal("default not used")
	}
	for _, value := range []string{"0", "201", "abc", ""} {
		if _, err := boundedQueryInt(url.Values{"limit": {value}}, "limit", 50, 1, 200); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("out of range value accepted: " + value)
		}
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
