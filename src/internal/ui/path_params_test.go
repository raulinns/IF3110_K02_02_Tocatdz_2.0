package ui

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestPathIdentifier(t *testing.T) {
	var got string
	var gotErr error
	mux := http.NewServeMux()
	mux.HandleFunc("GET /t/{table}", func(_ http.ResponseWriter, r *http.Request) {
		got, gotErr = pathIdentifier(r, "table")
	})
	for _, scenario := range []struct {
		target string
		want   string
		valid  bool
	}{
		{"/t/users", "users", true},
		{"/t/Order%20Items", "Order Items", true},
		{"/t/tabel_%C3%BCnik", "tabel_ünik", true},
		{"/t/" + strings.Repeat("a", 63), strings.Repeat("a", 63), true},
		{"/t/" + strings.Repeat("a", 64), "", false},
		{"/t/bad%00name", "", false},
		{"/t/bad%0Aname", "bad\nname", true},
		{"/t/%ff%fe", "", false},
	} {
		got, gotErr = "", nil
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", scenario.target, nil))
		if scenario.valid && (gotErr != nil || got != scenario.want) || !scenario.valid && !errors.Is(gotErr, security.ErrInvalidInput) {
			t.Fatal("wrong identifier handling for " + scenario.target)
		}
	}
}

func TestPathIdentifierRejectsMissingValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if _, err := pathIdentifier(r, "table"); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("missing path value accepted")
	}
	if _, err := pathID(r, "migrationID"); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("missing id accepted")
	}
}

func TestPathID(t *testing.T) {
	var got security.ID
	var gotErr error
	mux := http.NewServeMux()
	mux.HandleFunc("GET /m/{migrationID}", func(_ http.ResponseWriter, r *http.Request) {
		got, gotErr = pathID(r, "migrationID")
	})
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/m/"+string(testDatabaseA), nil))
	if gotErr != nil || got != testDatabaseA {
		t.Fatal("valid id rejected")
	}
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/m/1", nil))
	if !errors.Is(gotErr, security.ErrInvalidInput) || got != "" {
		t.Fatal("invalid id accepted")
	}
}
