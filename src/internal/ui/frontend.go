package ui

import (
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type FrontendConfig struct {
	Security   security.HTTPMiddlewareConfig
	Viewer     explorer.Viewer
	Authorizer security.Authorizer
	// Opsional sampai service-nya tersedia; selama nil route dan link-nya tidak dipasang
	Migrations MigrationService
	Admin      *AdminServices
}

type Frontend struct{ routes []security.RouteRegistrar }

var _ security.RouteRegistrar = (*Frontend)(nil)

// Semua halaman memakai middleware dan renderer yang sama supaya error keamanan tetap tampil konsisten.
func NewFrontend(cfg FrontendConfig) (*Frontend, error) {
	adapter, err := NewExplorerAdapter(cfg.Viewer)
	if err != nil {
		return nil, err
	}
	access, err := NewDatabaseGuard(cfg.Authorizer)
	if err != nil {
		return nil, err
	}
	renderer, err := NewRenderer()
	if err != nil {
		return nil, err
	}
	cfg.Security.ErrorWriter = func(w http.ResponseWriter, r *http.Request, err error) {
		_ = renderer.RenderError(w, r, err)
	}
	guard, err := security.NewHTTPMiddleware(cfg.Security)
	if err != nil {
		return nil, err
	}
	auth, err := NewAuthUI(cfg.Security.Auth, guard, cfg.Security.Cookies, renderer)
	if err != nil {
		return nil, err
	}
	databases, err := NewDatabaseUI(adapter, guard, renderer)
	if err != nil {
		return nil, err
	}
	tables, err := NewExplorerUI(adapter, guard, access, renderer)
	if err != nil {
		return nil, err
	}
	static, err := NewStaticAssets()
	if err != nil {
		return nil, err
	}
	routes := []security.RouteRegistrar{auth, databases, tables, static}
	var features Features
	if !missingDependency(cfg.Migrations) {
		migrations, err := NewMigrationUI(cfg.Migrations, guard, access, renderer)
		if err != nil {
			return nil, err
		}
		routes, features.Migrations = append(routes, migrations), true
	}
	if cfg.Admin != nil {
		admin, err := NewAdminUI(*cfg.Admin, guard, access, renderer)
		if err != nil {
			return nil, err
		}
		routes, features.Admin = append(routes, admin), true
	}
	renderer.features = features
	return &Frontend{routes: routes}, nil
}

func (f *Frontend) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/databases", http.StatusSeeOther)
	})
	for _, route := range f.routes {
		route.RegisterRoutes(mux)
	}
}
