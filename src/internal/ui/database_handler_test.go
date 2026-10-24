package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type fakeLister struct {
	calls     int
	principal security.Principal
	databases []DatabaseSummary
	err       error
}

func (l *fakeLister) ListAccessible(_ context.Context, principal security.Principal) ([]DatabaseSummary, error) {
	l.calls++
	l.principal = principal
	return l.databases, l.err
}

func testDatabaseUI(t *testing.T, lister *fakeLister) *http.ServeMux {
	t.Helper()
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	ui, err := NewDatabaseUI(lister, &fakeGuard{}, renderer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ui.RegisterRoutes(mux)
	return mux
}

func listRequest(principal bool) *http.Request {
	r := httptest.NewRequest("GET", "/databases", nil)
	if principal {
		r = r.WithContext(security.WithPrincipal(r.Context(), testPrincipal))
	}
	return r
}

func TestNewDatabaseUIRejectsMissingDependencies(t *testing.T) {
	renderer, _ := NewRenderer()
	var nilLister *fakeLister
	for _, build := range []func() (*DatabaseUI, error){
		func() (*DatabaseUI, error) { return NewDatabaseUI(nil, &fakeGuard{}, renderer) },
		func() (*DatabaseUI, error) { return NewDatabaseUI(nilLister, &fakeGuard{}, renderer) },
		func() (*DatabaseUI, error) { return NewDatabaseUI(&fakeLister{}, nil, renderer) },
		func() (*DatabaseUI, error) { return NewDatabaseUI(&fakeLister{}, &fakeGuard{}, nil) },
	} {
		if ui, err := build(); ui != nil || !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing dependency accepted")
		}
	}
}

func TestDatabaseListShowsAccessibleDatabases(t *testing.T) {
	lister := &fakeLister{databases: []DatabaseSummary{
		{ID: testDatabaseA, Name: "inventaris", Access: security.AccessViewOnly},
		{ID: testDatabaseB, Name: "keuangan", Access: security.AccessViewEdit},
	}}
	w := httptest.NewRecorder()
	testDatabaseUI(t, lister).ServeHTTP(w, listRequest(true))
	body := w.Body.String()
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("database list not rendered safely")
	}
	for _, want := range []string{"inventaris", "keuangan", "Lihat saja", "Lihat &#43; ubah", "/databases/" + string(testDatabaseA) + "/schema", "/databases/" + string(testDatabaseB) + "/migrations"} {
		if !strings.Contains(body, want) {
			t.Fatal("database list missing: " + want)
		}
	}
	if lister.calls != 1 || lister.principal != testPrincipal {
		t.Fatal("lister not called with principal from context")
	}
}

func TestDatabaseListEmptyState(t *testing.T) {
	w := httptest.NewRecorder()
	testDatabaseUI(t, &fakeLister{}).ServeHTTP(w, listRequest(true))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "belum memiliki akses") || strings.Contains(w.Body.String(), "<table") {
		t.Fatal("empty state not shown")
	}
}

func TestDatabaseListEscapesNames(t *testing.T) {
	lister := &fakeLister{databases: []DatabaseSummary{{ID: testDatabaseA, Name: "<script>alert(1)</script>", Access: security.AccessViewOnly}}}
	w := httptest.NewRecorder()
	testDatabaseUI(t, lister).ServeHTTP(w, listRequest(true))
	if strings.Contains(w.Body.String(), "<script>alert") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatal("database name not escaped")
	}
}

func TestDatabaseListRequiresPrincipal(t *testing.T) {
	lister := &fakeLister{databases: []DatabaseSummary{{ID: testDatabaseA, Name: "rahasia", Access: security.AccessViewOnly}}}
	w := httptest.NewRecorder()
	testDatabaseUI(t, lister).ServeHTTP(w, listRequest(false))
	if w.Code != http.StatusUnauthorized || lister.calls != 0 || strings.Contains(w.Body.String(), "rahasia") {
		t.Fatal("database list served without principal")
	}
}

func TestDatabaseListFailuresShowSafeError(t *testing.T) {
	for _, scenario := range []struct {
		err    error
		status int
	}{
		{security.ErrUnavailable, http.StatusServiceUnavailable},
		{security.ErrForbidden, http.StatusForbidden},
		{errors.New("dial postgres://admin:canary-secret@db"), http.StatusInternalServerError},
	} {
		w := httptest.NewRecorder()
		testDatabaseUI(t, &fakeLister{err: scenario.err}).ServeHTTP(w, listRequest(true))
		if w.Code != scenario.status || strings.Contains(w.Body.String(), "canary-secret") || strings.Contains(w.Body.String(), "<table") {
			t.Fatal("lister failure handled incorrectly")
		}
	}
}

func TestDatabaseListFailsClosedOnInvalidEntries(t *testing.T) {
	for _, bad := range []DatabaseSummary{
		{ID: "bukan-uuid", Name: "salah", Access: security.AccessViewOnly},
		{ID: testDatabaseA, Name: "", Access: security.AccessViewOnly},
		{ID: testDatabaseA, Name: "salah", Access: security.AccessNone},
		{ID: testDatabaseA, Name: "salah", Access: "admin"},
	} {
		lister := &fakeLister{databases: []DatabaseSummary{{ID: testDatabaseB, Name: "valid", Access: security.AccessViewOnly}, bad}}
		w := httptest.NewRecorder()
		testDatabaseUI(t, lister).ServeHTTP(w, listRequest(true))
		if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "valid") || strings.Contains(w.Body.String(), "salah") {
			t.Fatal("invalid entry rendered: " + bad.Name)
		}
	}
}
