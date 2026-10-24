package ui

import (
	"context"
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type DatabaseSummary struct {
	ID     security.ID
	Name   string
	Access security.DatabaseAccess
}

func (d DatabaseSummary) AccessLabel() string {
	if d.Access == security.AccessViewEdit {
		return "Lihat + ubah"
	}
	return "Lihat saja"
}

func (d DatabaseSummary) valid() bool {
	return security.ValidID(d.ID) && d.Name != "" && (d.Access == security.AccessViewOnly || d.Access == security.AccessViewEdit)
}

// Implementasi wajib hanya mengembalikan database yang punya grant untuk principal
type DatabaseLister interface {
	ListAccessible(ctx context.Context, principal security.Principal) ([]DatabaseSummary, error)
}

type DatabasesView struct {
	Databases []DatabaseSummary
}

type DatabaseUI struct {
	lister   DatabaseLister
	guard    security.Middleware
	renderer *Renderer
}

var _ security.RouteRegistrar = (*DatabaseUI)(nil)

func NewDatabaseUI(lister DatabaseLister, guard security.Middleware, renderer *Renderer) (*DatabaseUI, error) {
	for _, dependency := range []any{lister, guard, renderer} {
		if missingDependency(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &DatabaseUI{lister: lister, guard: guard, renderer: renderer}, nil
}

func (ui *DatabaseUI) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /databases", ui.guard.Protected(http.HandlerFunc(ui.list)))
}

func (ui *DatabaseUI) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		_ = ui.renderer.RenderError(w, r, security.ErrUnauthenticated)
		return
	}
	databases, err := ui.lister.ListAccessible(r.Context(), principal)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	// Entri tidak valid berarti sumber data rusak; tolak seluruh halaman daripada menampilkan akses yang tidak jelas
	for _, database := range databases {
		if !database.valid() {
			_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
			return
		}
	}
	_ = ui.renderer.Render(w, r, http.StatusOK, "databases", Page{Title: "Database", Content: DatabasesView{Databases: databases}})
}
