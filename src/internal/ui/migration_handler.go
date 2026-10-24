package ui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	maxMigrationTitleRunes = migration.MaxTitleRunes
	maxMigrationSQLBytes   = migration.MaxSQLBytes
)

type MigrationStatus string

const (
	StatusDraft   MigrationStatus = "draft"
	StatusApplied MigrationStatus = "applied"
	StatusFailed  MigrationStatus = "failed"
	// Hasil commit di database target belum pasti dan masih menunggu rekonsiliasi
	StatusPending MigrationStatus = "pending"
)

func (s MigrationStatus) Label() string {
	switch s {
	case StatusDraft:
		return "Draft"
	case StatusApplied:
		return "Berhasil (commit)"
	case StatusFailed:
		return "Gagal (rollback)"
	case StatusPending:
		return "Sedang diproses / menunggu konfirmasi"
	}
	return "Tidak diketahui"
}

func (s MigrationStatus) valid() bool {
	return s == StatusDraft || s == StatusApplied || s == StatusFailed || s == StatusPending
}

type MigrationSummary struct {
	ID         security.ID
	Version    int64
	Title      string
	Status     MigrationStatus
	Actor      string
	ExecutedAt time.Time
	UpdatedAt  time.Time
}

func (m MigrationSummary) VersionLabel() string {
	if m.Version <= 0 {
		return "-"
	}
	return strconv.FormatInt(m.Version, 10)
}

func (m MigrationSummary) ExecutedLabel() string { return timeLabel(m.ExecutedAt) }

func (m MigrationSummary) UpdatedLabel() string { return timeLabel(m.UpdatedAt) }

func timeLabel(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02 15:04:05") + " UTC"
}

type MigrationDetail struct {
	MigrationSummary
	SQL           string
	Revision      uint64
	FailureReason string
}

type MigrationDraft struct {
	Title string
	SQL   string
}

type MigrationHistoryPage struct {
	Items       []MigrationSummary
	Page, Limit int
	HasNext     bool
}

// Implementasi wajib memeriksa bahwa migration milik database di request dan mengotorisasi ulang sebelum eksekusi
type MigrationService interface {
	History(ctx context.Context, request DatabaseRequest, query migration.HistoryQuery) (MigrationHistoryPage, error)
	Migration(ctx context.Context, request DatabaseRequest, migrationID security.ID) (MigrationDetail, error)
	CreateDraft(ctx context.Context, request DatabaseRequest, draft MigrationDraft) (security.ID, error)
	UpdateDraft(ctx context.Context, request DatabaseRequest, migrationID security.ID, draft MigrationDraft, expectedRevision uint64) error
	Execute(ctx context.Context, request DatabaseRequest, migrationID security.ID, expectedRevision uint64) error
}

type MigrationForm struct {
	Action   string
	Title    string
	SQL      string
	Revision uint64
	Error    string
	Submit   string
}

type MigrationListView struct {
	DatabaseID security.ID
	Migrations []MigrationSummary
	CanEdit    bool
	Page       MigrationHistoryPage
}

func (v MigrationListView) historyHref(page int) string {
	return databasePath(v.DatabaseID, "migrations") + "?" + url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(v.Page.Limit)}}.Encode()
}
func (v MigrationListView) PrevHref() string {
	if v.Page.Page <= 1 {
		return ""
	}
	return v.historyHref(v.Page.Page - 1)
}
func (v MigrationListView) NextHref() string {
	if !v.Page.HasNext || v.Page.Limit <= 0 || int64(v.Page.Page) > migration.MaxOffset/int64(v.Page.Limit) {
		return ""
	}
	return v.historyHref(v.Page.Page + 1)
}

func (v MigrationListView) Tabs() DatabaseTabs { return databaseTabs(v.DatabaseID, "migrations") }

func (v MigrationListView) NewHref() string { return databasePath(v.DatabaseID, "migrations", "new") }

func (v MigrationListView) DetailHref(id security.ID) string {
	return databasePath(v.DatabaseID, "migrations", string(id))
}

type MigrationEditorView struct {
	DatabaseID security.ID
	Form       MigrationForm
}

func (v MigrationEditorView) Tabs() DatabaseTabs { return databaseTabs(v.DatabaseID, "migrations") }

type MigrationDetailView struct {
	DatabaseID security.ID
	Migration  MigrationDetail
	Editable   bool
	Form       MigrationForm
}

func (v MigrationDetailView) Tabs() DatabaseTabs { return databaseTabs(v.DatabaseID, "migrations") }

func (v MigrationDetailView) ExecuteAction() string {
	return databasePath(v.DatabaseID, "migrations", string(v.Migration.ID), "execute")
}

type MigrationUI struct {
	migrations MigrationService
	guard      security.Middleware
	access     *DatabaseGuard
	renderer   *Renderer
}

var _ security.RouteRegistrar = (*MigrationUI)(nil)

func NewMigrationUI(migrations MigrationService, guard security.Middleware, access *DatabaseGuard, renderer *Renderer) (*MigrationUI, error) {
	for _, dependency := range []any{migrations, guard, access, renderer} {
		if missingDependency(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &MigrationUI{migrations: migrations, guard: guard, access: access, renderer: renderer}, nil
}

func (ui *MigrationUI) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /databases/{databaseID}/migrations", ui.guard.Protected(http.HandlerFunc(ui.history)))
	mux.Handle("GET /databases/{databaseID}/migrations/new", ui.guard.Protected(http.HandlerFunc(ui.newDraft)))
	mux.Handle("POST /databases/{databaseID}/migrations", ui.guard.Protected(http.HandlerFunc(ui.create)))
	mux.Handle("GET /databases/{databaseID}/migrations/{migrationID}", ui.guard.Protected(http.HandlerFunc(ui.detail)))
	mux.Handle("POST /databases/{databaseID}/migrations/{migrationID}", ui.guard.Protected(http.HandlerFunc(ui.update)))
	mux.Handle("POST /databases/{databaseID}/migrations/{migrationID}/execute", ui.guard.Protected(http.HandlerFunc(ui.execute)))
}

func (ui *MigrationUI) history(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionViewHistory)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, security.ErrInvalidInput)
		return
	}
	query, err := migration.ParseHistoryQuery(values)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	page, err := ui.migrations.History(r.Context(), request, query)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	if page.Page != query.Page || page.Limit != query.Limit || len(page.Items) > query.Limit {
		_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
		return
	}
	for _, migration := range page.Items {
		if !security.ValidID(migration.ID) || !migration.Status.valid() {
			_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
			return
		}
	}
	view := MigrationListView{DatabaseID: request.DatabaseID, Migrations: page.Items, Page: page, CanEdit: ui.access.Allows(r, request, security.ActionCreateMigration)}
	_ = ui.renderer.Render(w, r, http.StatusOK, "migration/list", Page{Title: "Migration", Content: view})
}

func createForm(databaseID security.ID) MigrationForm {
	return MigrationForm{Action: databasePath(databaseID, "migrations"), Submit: "Simpan draft"}
}

func (ui *MigrationUI) newDraft(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionCreateMigration)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	ui.renderEditor(w, r, http.StatusOK, request.DatabaseID, createForm(request.DatabaseID))
}

func (ui *MigrationUI) renderEditor(w http.ResponseWriter, r *http.Request, status int, databaseID security.ID, form MigrationForm) {
	_ = ui.renderer.Render(w, r, status, "migration/editor", Page{Title: "Editor migration", Content: MigrationEditorView{DatabaseID: databaseID, Form: form}})
}

// SQL disimpan persis seperti yang diketik; spasi di awal/akhir hanya diabaikan untuk cek kosong
func parseDraft(values url.Values) (MigrationDraft, string) {
	title, ok := formText(values, "title", maxMigrationTitleRunes)
	if !ok {
		return MigrationDraft{}, "Judul wajib diisi, maksimal 120 karakter"
	}
	sql := values.Get("sql")
	if strings.TrimSpace(sql) == "" || len(sql) > maxMigrationSQLBytes || !utf8.ValidString(sql) {
		return MigrationDraft{}, "SQL wajib diisi, maksimal 512 KiB"
	}
	return MigrationDraft{Title: title, SQL: sql}, ""
}

// Pesan yang bisa diperbaiki user ditampilkan di editor beserta isian SQL-nya supaya tidak hilang
func formFailure(err error) (int, string, bool) {
	var reason migration.SQLRejection
	if errors.As(err, &reason) {
		return http.StatusBadRequest, reason.Message(), true
	}
	for _, category := range []error{security.ErrInvalidInput, security.ErrConflict} {
		if errors.Is(err, category) {
			status, message := security.HTTPStatus(err)
			return status, message, true
		}
	}
	return 0, "", false
}

func (ui *MigrationUI) create(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionCreateMigration)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	values, err := readForm(r, "title", "sql")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	form := createForm(request.DatabaseID)
	form.Title, form.SQL = values.Get("title"), values.Get("sql")
	draft, message := parseDraft(values)
	if message != "" {
		form.Error = message
		ui.renderEditor(w, r, http.StatusBadRequest, request.DatabaseID, form)
		return
	}
	id, err := ui.migrations.CreateDraft(r.Context(), request, draft)
	if status, message, ok := formFailure(err); ok {
		form.Error = message
		ui.renderEditor(w, r, status, request.DatabaseID, form)
		return
	}
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	if !security.ValidID(id) {
		_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
		return
	}
	http.Redirect(w, r, databasePath(request.DatabaseID, "migrations", string(id)), http.StatusSeeOther)
}

func (ui *MigrationUI) load(w http.ResponseWriter, r *http.Request, request DatabaseRequest) (MigrationDetail, bool) {
	migrationID, err := pathID(r, "migrationID")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return MigrationDetail{}, false
	}
	migration, err := ui.migrations.Migration(r.Context(), request, migrationID)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return MigrationDetail{}, false
	}
	if migration.ID != migrationID || !migration.Status.valid() || migration.Status == StatusDraft && migration.Revision == 0 {
		_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
		return MigrationDetail{}, false
	}
	return migration, true
}

func updateForm(databaseID security.ID, migration MigrationDetail) MigrationForm {
	return MigrationForm{
		Action: databasePath(databaseID, "migrations", string(migration.ID)), Title: migration.Title,
		SQL: migration.SQL, Revision: migration.Revision, Submit: "Simpan perubahan",
	}
}

func (ui *MigrationUI) detail(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionViewMigration)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	migration, ok := ui.load(w, r, request)
	if !ok {
		return
	}
	// Riwayat yang sudah dieksekusi tidak pernah bisa diedit
	editable := migration.Status == StatusDraft && ui.access.Allows(r, request, security.ActionUpdateMigration)
	view := MigrationDetailView{DatabaseID: request.DatabaseID, Migration: migration, Editable: editable, Form: updateForm(request.DatabaseID, migration)}
	_ = ui.renderer.Render(w, r, http.StatusOK, "migration/detail", Page{Title: "Migration " + migration.Title, Content: view})
}

func (ui *MigrationUI) update(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionUpdateMigration)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	migrationID, err := pathID(r, "migrationID")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	values, err := readForm(r, "title", "sql", "revision")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	revision, err := formRevision(values)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	// Revision lama dipertahankan saat gagal agar kirim ulang tanpa memuat ulang tetap ditolak
	form := MigrationForm{
		Action: databasePath(request.DatabaseID, "migrations", string(migrationID)), Title: values.Get("title"),
		SQL: values.Get("sql"), Revision: revision, Submit: "Simpan perubahan",
	}
	draft, message := parseDraft(values)
	if message != "" {
		form.Error = message
		ui.renderEditor(w, r, http.StatusBadRequest, request.DatabaseID, form)
		return
	}
	err = ui.migrations.UpdateDraft(r.Context(), request, migrationID, draft, revision)
	if status, message, ok := formFailure(err); ok {
		form.Error = message
		ui.renderEditor(w, r, status, request.DatabaseID, form)
		return
	}
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	http.Redirect(w, r, databasePath(request.DatabaseID, "migrations", string(migrationID)), http.StatusSeeOther)
}

func (ui *MigrationUI) execute(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionExecuteMigration)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	migrationID, err := pathID(r, "migrationID")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	values, err := readForm(r, "revision")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	revision, err := formRevision(values)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	// Gagal di SQL dicatat sebagai status failed oleh service; error di sini berarti eksekusi tidak dimulai atau hasilnya belum pasti
	if err := ui.migrations.Execute(r.Context(), request, migrationID, revision); err != nil {
		if errors.Is(err, migration.ErrExecutionUncertain) {
			_ = ui.renderer.Render(w, r, http.StatusServiceUnavailable, "migration/pending", Page{Title: "Hasil migration belum pasti", Content: MigrationPendingView{DetailHref: databasePath(request.DatabaseID, "migrations", string(migrationID))}})
			return
		}
		if status, message, ok := formFailure(err); ok {
			_ = ui.renderer.Render(w, r, status, "migration/rejected", Page{Title: "SQL migration ditolak", Content: MigrationRejectedView{DetailHref: databasePath(request.DatabaseID, "migrations", string(migrationID)), Message: message}})
			return
		}
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	http.Redirect(w, r, databasePath(request.DatabaseID, "migrations", string(migrationID)), http.StatusSeeOther)
}

type MigrationPendingView struct{ DetailHref string }
type MigrationRejectedView struct{ DetailHref, Message string }
