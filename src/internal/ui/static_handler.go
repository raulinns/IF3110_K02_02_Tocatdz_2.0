package ui

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type StaticAssets struct {
	root  fs.FS
	files http.Handler
}

var _ security.RouteRegistrar = (*StaticAssets)(nil)

func NewStaticAssets() (*StaticAssets, error) { return newStaticAssets(assets) }

func newStaticAssets(files fs.FS) (*StaticAssets, error) {
	root, err := fs.Sub(files, "static")
	if err != nil {
		return nil, err
	}
	return &StaticAssets{root: root, files: http.StripPrefix("/static/", http.FileServerFS(root))}, nil
}

func (s *StaticAssets) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /static/", s)
}

// Direktori tidak pernah ditampilkan, hanya file yang ada
func (s *StaticAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info, err := fs.Stat(s.root, strings.TrimPrefix(r.URL.Path, "/static/"))
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	s.files.ServeHTTP(w, r)
}
