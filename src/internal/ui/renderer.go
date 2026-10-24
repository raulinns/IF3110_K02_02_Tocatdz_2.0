package ui

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	pagesDir       = "templates/pages"
	layoutTemplate = "base"
)

var ErrTemplateNotFound = errors.New("template not found")

type Page struct {
	Title    string
	Content  any
	Identity security.Identity
	SignedIn bool
	CSRF security.Secret // Diekspor agar fmt manggil Secret.Format dan tidak cetak isi token
}

// Token CSRF hanya dibuka saat dimasukkan ke hidden field form
func (p Page) CSRFToken() string { return p.CSRF.Reveal() }

type viewer struct {
	identity security.Identity
	csrf     security.Secret
	signedIn bool
}

func viewerFromContext(ctx context.Context) viewer {
	identity, signedIn := security.IdentityFromContext(ctx)
	csrf, _ := security.CSRFTokenFromContext(ctx)
	return viewer{identity: identity, csrf: csrf, signedIn: signedIn}
}

type Renderer struct {
	pages  map[string]*template.Template
	viewer func(context.Context) viewer
}

func NewRenderer() (*Renderer, error) { return newRenderer(assets) }

func newRenderer(files fs.FS) (*Renderer, error) {
	pages := map[string]*template.Template{}
	err := fs.WalkDir(files, pagesDir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Ext(name) != ".html" {
			return err
		}
		page := strings.TrimSuffix(strings.TrimPrefix(name, pagesDir+"/"), ".html")
		tmpl, err := template.New(page).ParseFS(files, "templates/layouts/*.html", "templates/partials/*.html", name)
		if err != nil {
			return err
		}
		if tmpl.Lookup(layoutTemplate) == nil {
			return ErrTemplateNotFound
		}
		pages[page] = tmpl
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, ErrTemplateNotFound
	}
	return &Renderer{pages: pages, viewer: viewerFromContext}, nil
}

// Identity dan CSRF diambil dari context middleware
func (r *Renderer) Render(w http.ResponseWriter, req *http.Request, status int, name string, page Page) error {
	tmpl, ok := r.pages[name]
	if !ok {
		renderFailure(w)
		return ErrTemplateNotFound
	}
	current := r.viewer(req.Context())
	page.Identity, page.SignedIn, page.CSRF = current.identity, current.signedIn, current.csrf
	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, layoutTemplate, page); err != nil {
		renderFailure(w)
		return err
	}
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, err := body.WriteTo(w)
	return err
}

func renderFailure(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Terjadi kesalahan", http.StatusInternalServerError)
}
