package ui

import (
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type ErrorView struct {
	Status  int
	Message string
}

func errorResponse(err error) (int, string) {
	return security.HTTPStatus(err)
}

func (r *Renderer) RenderError(w http.ResponseWriter, req *http.Request, err error) error {
	status, message := errorResponse(err)
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "60")
	}
	return r.Render(w, req, status, "error", Page{Title: message, Content: ErrorView{Status: status, Message: message}})
}
