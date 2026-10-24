package ui

import (
	"errors"
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type ErrorView struct {
	Status  int
	Message string
}

func (r *Renderer) RenderError(w http.ResponseWriter, req *http.Request, err error) error {
	status, message := security.HTTPStatus(err)
	if errors.Is(err, security.ErrRateLimited) {
		w.Header().Set("Retry-After", "60")
	}
	return r.Render(w, req, status, "error", Page{Title: message, Content: ErrorView{Status: status, Message: message}})
}

// Dipasang sebagai HTTPMiddlewareConfig.ErrorWriter; status dan header keamanan tetap diatur middleware
func (r *Renderer) WriteError(w http.ResponseWriter, req *http.Request, err error) {
	_ = r.RenderError(w, req, err)
}
