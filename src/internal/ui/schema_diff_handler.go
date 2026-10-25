package ui

import (
	"errors"
	"net/http"
	"net/url"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const defaultDiffSchema = "public"

type SchemaDiffView struct {
	DatabaseID   security.ID
	Databases    []DatabaseSummary
	Schema       string
	Source       security.ID
	SourceSchema string
	Error        string
	Compared     bool
	CurrentName  string
	SourceName   string
	Changes      []schemadiff.Change
	Warnings     []string
	Added        int
	Dropped      int
	Altered      int
	Destructive  int
	SQL          string `json:"-"`
	Validation   string
	CanDraft     bool
	Form         MigrationForm
}

type SchemaDiffStat struct {
	Icon, Label, Note string
	Value             int
}

func (v SchemaDiffView) Stats() []SchemaDiffStat {
	return []SchemaDiffStat{
		{Icon: "fa-plus teal", Label: "Tambah", Value: v.Added, Note: "Objek baru"},
		{Icon: "fa-pen olive", Label: "Ubah", Value: v.Altered, Note: "Definisi berbeda"},
		{Icon: "fa-minus orange", Label: "Hapus", Value: v.Dropped, Note: "Tidak di pembanding"},
		{Icon: "fa-triangle-exclamation grey", Label: "Berisiko", Value: v.Destructive, Note: "Data bisa hilang"},
	}
}

func (v SchemaDiffView) Tabs() DatabaseTabs { return databaseTabs(v.DatabaseID, "diff") }

func (v SchemaDiffView) Action() string { return databasePath(v.DatabaseID, "schema-diff") }

type SchemaDiffUI struct {
	diff     schemadiff.Application
	lister   DatabaseLister
	guard    security.Middleware
	access   *DatabaseGuard
	renderer *Renderer
}

var _ security.RouteRegistrar = (*SchemaDiffUI)(nil)

func NewSchemaDiffUI(diff schemadiff.Application, lister DatabaseLister, guard security.Middleware, access *DatabaseGuard, renderer *Renderer) (*SchemaDiffUI, error) {
	for _, dependency := range []any{diff, lister, guard, access, renderer} {
		if missingDependency(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &SchemaDiffUI{diff: diff, lister: lister, guard: guard, access: access, renderer: renderer}, nil
}

func (ui *SchemaDiffUI) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /databases/{databaseID}/schema-diff", ui.guard.Protected(http.HandlerFunc(ui.compare)))
}

// Perbandingan hanya membaca katalog, jadi memakai GET; kandidat SQL disimpan lewat form draft migration yang sudah ada.
func (ui *SchemaDiffUI) compare(w http.ResponseWriter, r *http.Request) {
	request, err := ui.access.Authorize(r, security.ActionViewSchema)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, security.ErrInvalidInput)
		return
	}
	for key, items := range values {
		if (key != "schema" && key != "source" && key != "source_schema") || len(items) != 1 {
			_ = ui.renderer.RenderError(w, r, security.ErrInvalidInput)
			return
		}
	}
	databases, err := ui.lister.ListAccessible(r.Context(), request.Principal)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	view := SchemaDiffView{DatabaseID: request.DatabaseID, Databases: databases, Schema: defaultDiffSchema, SourceSchema: defaultDiffSchema}
	names := map[security.ID]string{}
	for _, database := range databases {
		if !database.valid() {
			_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
			return
		}
		names[database.ID] = database.Name
		if view.Source == "" && database.ID != request.DatabaseID {
			view.Source = database.ID
		}
	}
	view.CurrentName = names[request.DatabaseID]
	if view.Source == "" {
		view.Source = request.DatabaseID
	}
	if len(values) == 0 {
		_ = ui.renderer.Render(w, r, http.StatusOK, "schema/diff", Page{Title: "Schema diff", Content: view})
		return
	}
	view.Schema, view.Source, view.SourceSchema = values.Get("schema"), security.ID(values.Get("source")), values.Get("source_schema")
	if !utf8.ValidString(view.Schema) || !utf8.ValidString(view.SourceSchema) {
		_ = ui.renderer.RenderError(w, r, security.ErrInvalidInput)
		return
	}
	if !security.ValidID(view.Source) || names[view.Source] == "" {
		view.Error = "Pilih database pembanding dari daftar database yang bisa Anda akses."
		_ = ui.renderer.Render(w, r, http.StatusBadRequest, "schema/diff", Page{Title: "Schema diff", Content: view})
		return
	}
	if !schemadiff.ValidSchemaName(view.Schema) || !schemadiff.ValidSchemaName(view.SourceSchema) {
		view.Error = "Nama schema wajib diisi, maksimal 63 byte, dan tidak boleh schema sistem."
		_ = ui.renderer.Render(w, r, http.StatusBadRequest, "schema/diff", Page{Title: "Schema diff", Content: view})
		return
	}
	if view.Source == request.DatabaseID && view.Schema == view.SourceSchema {
		view.Error = "Schema pembanding sama dengan schema saat ini. Pilih database atau schema lain."
		_ = ui.renderer.Render(w, r, http.StatusBadRequest, "schema/diff", Page{Title: "Schema diff", Content: view})
		return
	}
	result, err := ui.diff.Compare(r.Context(), schemadiff.Request{Principal: request.Principal, CurrentID: request.DatabaseID, CurrentSchema: view.Schema, DesiredID: view.Source, DesiredSchema: view.SourceSchema})
	if err != nil {
		status, message := http.StatusBadRequest, ""
		switch {
		case errors.Is(err, security.ErrNotFound):
			status, message = http.StatusNotFound, "Schema tidak ditemukan pada database saat ini atau database pembanding."
		case errors.Is(err, security.ErrResultTooLarge):
			status, message = http.StatusUnprocessableEntity, "Schema terlalu besar untuk dibandingkan di halaman ini."
		case errors.Is(err, security.ErrInvalidInput):
			message = "Permintaan perbandingan tidak valid. Periksa database dan nama schema."
		}
		if message == "" {
			_ = ui.renderer.RenderError(w, r, err)
			return
		}
		view.Error = message
		_ = ui.renderer.Render(w, r, status, "schema/diff", Page{Title: "Schema diff", Content: view})
		return
	}
	view.Compared, view.SourceName = true, names[view.Source]
	view.Changes, view.Warnings, view.Destructive, view.Validation = result.Changes, result.Warnings, result.Destructive, result.Validation
	view.SQL = result.SQL.Reveal()
	for _, change := range result.Changes {
		switch change.Action {
		case schemadiff.ActionAdd:
			view.Added++
		case schemadiff.ActionDrop:
			view.Dropped++
		default:
			view.Altered++
		}
	}
	view.CanDraft = view.SQL != "" && ui.renderer.feature("migrations") && ui.access.Allows(r, request, security.ActionCreateMigration)
	if view.CanDraft {
		view.Form = createForm(request.DatabaseID)
		view.Form.Title = draftTitle(view.Schema, view.SourceName, view.SourceSchema)
		view.Form.SQL = view.SQL
		view.Form.Submit = "Simpan sebagai draft"
	}
	_ = ui.renderer.Render(w, r, http.StatusOK, "schema/diff", Page{Title: "Schema diff", Content: view})
}

func draftTitle(schema, source, sourceSchema string) string {
	title := []rune("Sinkronkan " + schema + " dengan " + source + "." + sourceSchema)
	if len(title) > maxMigrationTitleRunes {
		title = title[:maxMigrationTitleRunes]
	}
	return string(title)
}
