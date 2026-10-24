package ui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const testMigrationID security.ID = "c3d4e5f6-a7b8-4c9d-8e0f-1a2b3c4d5e6f"

type fakeMigrations struct {
	calls       int
	request     DatabaseRequest
	migrationID security.ID
	draft       MigrationDraft
	revision    uint64
	history     []MigrationSummary
	detail      MigrationDetail
	createdID   security.ID
	err         error
}

func (m *fakeMigrations) History(_ context.Context, request DatabaseRequest, query migration.HistoryQuery) (MigrationHistoryPage, error) {
	m.calls++
	m.request = request
	return MigrationHistoryPage{Items: m.history, Page: query.Page, Limit: query.Limit}, m.err
}

func (m *fakeMigrations) Migration(_ context.Context, request DatabaseRequest, id security.ID) (MigrationDetail, error) {
	m.calls++
	m.request, m.migrationID = request, id
	return m.detail, m.err
}

func (m *fakeMigrations) CreateDraft(_ context.Context, request DatabaseRequest, draft MigrationDraft) (security.ID, error) {
	m.calls++
	m.request, m.draft = request, draft
	return m.createdID, m.err
}

func (m *fakeMigrations) UpdateDraft(_ context.Context, request DatabaseRequest, id security.ID, draft MigrationDraft, revision uint64) error {
	m.calls++
	m.request, m.migrationID, m.draft, m.revision = request, id, draft, revision
	return m.err
}

func (m *fakeMigrations) Execute(_ context.Context, request DatabaseRequest, id security.ID, revision uint64) error {
	m.calls++
	m.request, m.migrationID, m.revision = request, id, revision
	return m.err
}

func testMigrationUI(t *testing.T, migrations *fakeMigrations, authorizer *fakeAuthorizer) *http.ServeMux {
	t.Helper()
	renderer, _ := NewRenderer()
	access, _ := NewDatabaseGuard(authorizer)
	ui, err := NewMigrationUI(migrations, &fakeGuard{}, access, renderer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ui.RegisterRoutes(mux)
	return mux
}

var (
	migrationsPath = "/databases/" + string(testDatabaseA) + "/migrations"
	migrationPath  = migrationsPath + "/" + string(testMigrationID)
	executePath    = migrationPath + "/execute"
	viewOnly       = map[security.DatabaseAction]bool{security.ActionCreateMigration: true, security.ActionUpdateMigration: true, security.ActionExecuteMigration: true}
)

func draftDetail() MigrationDetail {
	return MigrationDetail{
		MigrationSummary: MigrationSummary{ID: testMigrationID, Title: "Tambah kolom", Status: StatusDraft, UpdatedAt: time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)},
		SQL:              "ALTER TABLE users ADD COLUMN phone text;", Revision: 3,
	}
}

func TestNewMigrationUIRejectsMissingDependencies(t *testing.T) {
	renderer, _ := NewRenderer()
	access, _ := NewDatabaseGuard(&fakeAuthorizer{})
	for _, build := range []func() (*MigrationUI, error){
		func() (*MigrationUI, error) { return NewMigrationUI(nil, &fakeGuard{}, access, renderer) },
		func() (*MigrationUI, error) { return NewMigrationUI(&fakeMigrations{}, nil, access, renderer) },
		func() (*MigrationUI, error) { return NewMigrationUI(&fakeMigrations{}, &fakeGuard{}, nil, renderer) },
		func() (*MigrationUI, error) { return NewMigrationUI(&fakeMigrations{}, &fakeGuard{}, access, nil) },
	} {
		if ui, err := build(); ui != nil || !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing dependency accepted")
		}
	}
}

func TestHistoryShowsMigrationsAndEditButtonOnlyForEditors(t *testing.T) {
	executed := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	migrations := &fakeMigrations{history: []MigrationSummary{
		{ID: testMigrationID, Version: 4, Title: "Buat tabel", Status: StatusApplied, Actor: "budi", ExecutedAt: executed},
		{ID: testDatabaseB, Title: "Draft baru", Status: StatusDraft},
	}}
	w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("GET", migrationsPath, nil))
	body := w.Body.String()
	for _, want := range []string{"Buat tabel", "Berhasil (commit)", "budi", "2026-10-24 09:30:00 UTC", "Draft baru", migrationsPath + "/new", migrationPath} {
		if !strings.Contains(body, want) {
			t.Fatal("history missing: " + want)
		}
	}
	if w.Code != http.StatusOK || migrations.request.DatabaseID != testDatabaseA {
		t.Fatal("history not loaded for requested database")
	}
	w = serve(testMigrationUI(t, migrations, &fakeAuthorizer{denied: viewOnly}), protectedRequest("GET", migrationsPath, nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), migrationsPath+"/new") {
		t.Fatal("view only user offered create button")
	}
}

func TestHistoryFailsClosedOnInvalidEntries(t *testing.T) {
	for _, bad := range []MigrationSummary{{ID: "x", Status: StatusDraft}, {ID: testMigrationID, Status: "deleted"}} {
		w := serve(testMigrationUI(t, &fakeMigrations{history: []MigrationSummary{bad}}, &fakeAuthorizer{}), protectedRequest("GET", migrationsPath, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatal("invalid history entry rendered")
		}
	}
}

func TestDetailOffersEditorOnlyForEditableDrafts(t *testing.T) {
	w := serve(testMigrationUI(t, &fakeMigrations{detail: draftDetail()}, &fakeAuthorizer{}), protectedRequest("GET", migrationPath, nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `name="revision" value="3"`) || !strings.Contains(body, `action="`+executePath+`"`) || !strings.Contains(body, "<textarea") {
		t.Fatal("editable draft missing editor or execute form")
	}
	w = serve(testMigrationUI(t, &fakeMigrations{detail: draftDetail()}, &fakeAuthorizer{denied: viewOnly}), protectedRequest("GET", migrationPath, nil))
	if strings.Contains(w.Body.String(), "<textarea") || strings.Contains(w.Body.String(), executePath) || !strings.Contains(w.Body.String(), "ADD COLUMN phone") {
		t.Fatal("view only user offered edit controls or missing SQL")
	}
	applied := draftDetail()
	applied.Status, applied.Version, applied.Actor = StatusApplied, 5, "budi"
	w = serve(testMigrationUI(t, &fakeMigrations{detail: applied}, &fakeAuthorizer{}), protectedRequest("GET", migrationPath, nil))
	if strings.Contains(w.Body.String(), "<textarea") || strings.Contains(w.Body.String(), executePath) || !strings.Contains(w.Body.String(), "<pre") {
		t.Fatal("executed migration became editable")
	}
}

func TestDetailRejectsMigrationFromAnotherRequest(t *testing.T) {
	other := draftDetail()
	other.ID = testDatabaseB
	migrations := &fakeMigrations{detail: other}
	w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("GET", migrationPath, nil))
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "ADD COLUMN") || migrations.migrationID != testMigrationID {
		t.Fatal("mismatched migration rendered")
	}
	w = serve(testMigrationUI(t, &fakeMigrations{}, &fakeAuthorizer{}), protectedRequest("GET", migrationsPath+"/bukan-uuid", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatal("malformed migration id accepted")
	}
}

func TestDetailEscapesSQL(t *testing.T) {
	detail := draftDetail()
	detail.Status = StatusFailed
	detail.SQL = "SELECT '</pre><script>alert(1)</script>';"
	w := serve(testMigrationUI(t, &fakeMigrations{detail: detail}, &fakeAuthorizer{}), protectedRequest("GET", migrationPath, nil))
	if strings.Contains(w.Body.String(), "<script>alert") {
		t.Fatal("migration SQL not escaped")
	}
}

func TestCreateDraftRedirectsToDetail(t *testing.T) {
	migrations := &fakeMigrations{createdID: testMigrationID}
	sql := "  CREATE TABLE a (id int);\r\n"
	w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("POST", migrationsPath, url.Values{"title": {"  Buat tabel a "}, "sql": {sql}, "csrf_token": {"t"}}))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != migrationPath {
		t.Fatal("created draft did not redirect to detail")
	}
	if migrations.draft != (MigrationDraft{Title: "Buat tabel a", SQL: sql}) || migrations.request.Principal != testPrincipal {
		t.Fatal("draft not passed exactly to service")
	}
}

func TestCreateDraftValidationKeepsInput(t *testing.T) {
	for _, form := range []url.Values{
		{"title": {""}, "sql": {"SELECT 1;"}},
		{"title": {strings.Repeat("a", 121)}, "sql": {"SELECT 1;"}},
		{"title": {"Judul"}, "sql": {"   \n"}},
		{"title": {"Judul"}, "sql": {strings.Repeat("x", maxMigrationSQLBytes+1)}},
	} {
		migrations := &fakeMigrations{}
		w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("POST", migrationsPath, form))
		if w.Code != http.StatusBadRequest || migrations.calls != 0 || !strings.Contains(w.Body.String(), "notice-error") {
			t.Fatal("invalid draft reached service")
		}
	}
	w := serve(testMigrationUI(t, &fakeMigrations{}, &fakeAuthorizer{}), protectedRequest("POST", migrationsPath, url.Values{"title": {""}, "sql": {"DROP TABLE penting;"}}))
	if !strings.Contains(w.Body.String(), "DROP TABLE penting;") {
		t.Fatal("typed SQL lost after validation error")
	}
}

func TestCreateDraftServiceErrors(t *testing.T) {
	for _, scenario := range []struct {
		err    error
		status int
		editor bool
	}{
		{security.ErrInvalidInput, http.StatusBadRequest, true},
		{security.ErrConflict, http.StatusConflict, true},
		{security.ErrUnavailable, http.StatusServiceUnavailable, false},
		{errors.New("syntax error near canary-secret"), http.StatusInternalServerError, false},
	} {
		w := serve(testMigrationUI(t, &fakeMigrations{err: scenario.err}, &fakeAuthorizer{}), protectedRequest("POST", migrationsPath, url.Values{"title": {"Judul"}, "sql": {"SELECT 1;"}}))
		if w.Code != scenario.status || strings.Contains(w.Body.String(), "<textarea") != scenario.editor || strings.Contains(w.Body.String(), "canary-secret") {
			t.Fatal("create failure handled incorrectly")
		}
	}
	w := serve(testMigrationUI(t, &fakeMigrations{createdID: "bukan-uuid"}, &fakeAuthorizer{}), protectedRequest("POST", migrationsPath, url.Values{"title": {"Judul"}, "sql": {"SELECT 1;"}}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("invalid created id used in redirect")
	}
}

func TestUpdateDraftPassesRevisionAndKeepsInputOnConflict(t *testing.T) {
	migrations := &fakeMigrations{}
	form := url.Values{"title": {"Judul"}, "sql": {"SELECT 2;"}, "revision": {"3"}}
	w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("POST", migrationPath, form))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != migrationPath || migrations.revision != 3 || migrations.migrationID != testMigrationID {
		t.Fatal("update not passed with revision")
	}
	w = serve(testMigrationUI(t, &fakeMigrations{err: security.ErrConflict}, &fakeAuthorizer{}), protectedRequest("POST", migrationPath, form))
	body := w.Body.String()
	if w.Code != http.StatusConflict || !strings.Contains(body, "SELECT 2;") || !strings.Contains(body, `name="revision" value="3"`) || !strings.Contains(body, "Data berubah") {
		t.Fatal("conflict lost user input or revision")
	}
	for _, revision := range []string{"", "0", "x"} {
		migrations := &fakeMigrations{}
		w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("POST", migrationPath, url.Values{"title": {"Judul"}, "sql": {"SELECT 2;"}, "revision": {revision}}))
		if w.Code != http.StatusBadRequest || migrations.calls != 0 {
			t.Fatal("invalid revision reached service")
		}
	}
}

func TestExecuteRedirectsAndPropagatesFailure(t *testing.T) {
	migrations := &fakeMigrations{}
	w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("POST", executePath, url.Values{"revision": {"3"}}))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != migrationPath || migrations.revision != 3 {
		t.Fatal("execute not dispatched")
	}
	w = serve(testMigrationUI(t, &fakeMigrations{err: security.ErrConflict}, &fakeAuthorizer{}), protectedRequest("POST", executePath, url.Values{"revision": {"3"}}))
	if w.Code != http.StatusConflict {
		t.Fatal("stale execute not reported")
	}
	migrations = &fakeMigrations{}
	w = serve(testMigrationUI(t, migrations, &fakeAuthorizer{}), protectedRequest("POST", executePath, url.Values{"revision": {"3"}, "sql": {"DROP TABLE x;"}}))
	if w.Code != http.StatusBadRequest || migrations.calls != 0 {
		t.Fatal("execute accepted extra SQL field")
	}
}

func TestViewOnlyCannotChangeMigrations(t *testing.T) {
	for _, scenario := range []struct {
		method string
		target string
		form   url.Values
	}{
		{"GET", migrationsPath + "/new", nil},
		{"POST", migrationsPath, url.Values{"title": {"Judul"}, "sql": {"SELECT 1;"}}},
		{"POST", migrationPath, url.Values{"title": {"Judul"}, "sql": {"SELECT 1;"}, "revision": {"3"}}},
		{"POST", executePath, url.Values{"revision": {"3"}}},
	} {
		migrations := &fakeMigrations{}
		w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{denied: viewOnly}), protectedRequest(scenario.method, scenario.target, scenario.form))
		if w.Code != http.StatusForbidden || migrations.calls != 0 {
			t.Fatal("view only user changed migration: " + scenario.method + " " + scenario.target)
		}
	}
}

func TestMigrationViewsDeniedWithoutGrant(t *testing.T) {
	for _, target := range []string{migrationsPath, migrationPath} {
		migrations := &fakeMigrations{}
		w := serve(testMigrationUI(t, migrations, &fakeAuthorizer{databaseErr: security.ErrForbidden}), protectedRequest("GET", target, nil))
		if w.Code != http.StatusForbidden || migrations.calls != 0 {
			t.Fatal("migration view served without grant: " + target)
		}
	}
}
