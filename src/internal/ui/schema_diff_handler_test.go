package ui

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var diffPath = "/databases/" + string(testDatabaseA) + "/schema-diff"

type fakeDiff struct {
	request schemadiff.Request
	result  schemadiff.Result
	err     error
	calls   int
}

func (f *fakeDiff) Compare(_ context.Context, request schemadiff.Request) (schemadiff.Result, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func testDiffUI(t *testing.T, diff *fakeDiff, authorizer *fakeAuthorizer, features Features) *http.ServeMux {
	t.Helper()
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	renderer.features = features
	access, _ := NewDatabaseGuard(authorizer)
	lister := &fakeLister{databases: []DatabaseSummary{
		{ID: testDatabaseA, Name: "produksi", Access: security.AccessViewEdit},
		{ID: testDatabaseB, Name: "staging", Access: security.AccessViewOnly},
	}}
	ui, err := NewSchemaDiffUI(diff, lister, &fakeGuard{}, access, renderer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ui.RegisterRoutes(mux)
	return mux
}

func diffResult() schemadiff.Result {
	return schemadiff.Result{
		Changes:     []schemadiff.Change{{Action: schemadiff.ActionDrop, Object: "Tabel", Name: "<b>legacy</b>", Destructive: true}},
		Warnings:    []string{"Tipe <i>berubah</i>"},
		SQL:         security.NewSecret("DROP TABLE public.legacy;\n-- </textarea><script>alert(1)</script>"),
		Destructive: 1,
	}
}

func TestSchemaDiffFormDefaultsWithoutComparing(t *testing.T) {
	diff := &fakeDiff{}
	w := serve(testDiffUI(t, diff, &fakeAuthorizer{}, Features{Migrations: true, SchemaDiff: true}), protectedRequest("GET", diffPath, nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || diff.calls != 0 {
		t.Fatalf("form page failed: %d calls=%d", w.Code, diff.calls)
	}
	if !strings.Contains(body, `value="`+string(testDatabaseB)+`" selected`) || !strings.Contains(body, "Schema diff") || !strings.Contains(body, `href="`+diffPath+`"`) {
		t.Fatal("other database not preselected or tab missing")
	}
}

func TestSchemaDiffShowsEscapedResultAndEditableDraft(t *testing.T) {
	diff := &fakeDiff{result: diffResult()}
	target := diffPath + "?schema=public&source=" + string(testDatabaseB) + "&source_schema=public"
	w := serve(testDiffUI(t, diff, &fakeAuthorizer{}, Features{Migrations: true, SchemaDiff: true}), protectedRequest("GET", target, nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || diff.request.CurrentID != testDatabaseA || diff.request.DesiredID != testDatabaseB || diff.request.Principal != testPrincipal {
		t.Fatalf("compare request wrong: %d %+v", w.Code, diff.request)
	}
	if strings.Contains(body, "<b>legacy") || strings.Contains(body, "<script>alert") || strings.Contains(body, "<i>berubah") {
		t.Fatal("diff output not escaped")
	}
	for _, want := range []string{`action="/databases/` + string(testDatabaseA) + `/migrations"`, `name="sql"`, "DROP TABLE public.legacy;", "Sinkronkan public dengan staging.public", "Simpan sebagai draft"} {
		if !strings.Contains(body, want) {
			t.Fatalf("draft form missing %q", want)
		}
	}
}

func TestSchemaDiffWithoutCreateAccessIsReadOnly(t *testing.T) {
	diff := &fakeDiff{result: diffResult()}
	target := diffPath + "?schema=public&source=" + string(testDatabaseB) + "&source_schema=public"
	authorizer := &fakeAuthorizer{denied: map[security.DatabaseAction]bool{security.ActionCreateMigration: true}}
	body := serve(testDiffUI(t, diff, authorizer, Features{Migrations: true, SchemaDiff: true}), protectedRequest("GET", target, nil)).Body.String()
	if strings.Contains(body, `name="sql"`) || !strings.Contains(body, "Hanya baca") {
		t.Fatal("draft form shown without migration create access")
	}
}

func TestSchemaDiffValidatesQuery(t *testing.T) {
	diff := &fakeDiff{result: diffResult()}
	mux := testDiffUI(t, diff, &fakeAuthorizer{}, Features{Migrations: true, SchemaDiff: true})
	for target, want := range map[string]int{
		diffPath + "?schema=public&source=" + string(testDatabaseC) + "&source_schema=public":     http.StatusBadRequest,
		diffPath + "?schema=pg_catalog&source=" + string(testDatabaseB) + "&source_schema=public": http.StatusBadRequest,
		diffPath + "?schema=public&source=" + string(testDatabaseA) + "&source_schema=public":     http.StatusBadRequest,
		diffPath + "?schema=public&source=" + string(testDatabaseB) + "&extra=1":                  http.StatusBadRequest,
		diffPath + "?schema=a&schema=b&source=" + string(testDatabaseB) + "&source_schema=public": http.StatusBadRequest,
	} {
		if w := serve(mux, protectedRequest("GET", target, nil)); w.Code != want {
			t.Fatalf("%s: got %d want %d", target, w.Code, want)
		}
	}
	if diff.calls != 0 {
		t.Fatal("invalid query reached compare service")
	}
	diff.err = security.ErrNotFound
	if w := serve(mux, protectedRequest("GET", diffPath+"?schema=x&source="+string(testDatabaseB)+"&source_schema=public", nil)); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "Schema tidak ditemukan") {
		t.Fatal("missing schema not explained on form")
	}
	denied := &fakeAuthorizer{denied: map[security.DatabaseAction]bool{security.ActionViewSchema: true}}
	if w := serve(testDiffUI(t, &fakeDiff{}, denied, Features{SchemaDiff: true}), protectedRequest("GET", diffPath, nil)); w.Code != http.StatusForbidden {
		t.Fatalf("schema diff opened without schema access: %d", w.Code)
	}
}
