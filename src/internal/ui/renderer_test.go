package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const testCSRF = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func testFiles(page string) fstest.MapFS {
	return fstest.MapFS{
		"templates/layouts/base.html":    {Data: []byte(`{{define "base"}}<title>{{.Title}}</title>{{template "navbar" .}}{{template "content" .}}{{end}}`)},
		"templates/partials/navbar.html": {Data: []byte(`{{define "navbar"}}{{if .SignedIn}}{{.Identity.Username}}|{{.CSRFToken}}|{{end}}{{end}}`)},
		"templates/pages/test.html":      {Data: []byte(page)},
	}
}

func testRenderer(t *testing.T, page string) *Renderer {
	t.Helper()
	r, err := newRenderer(testFiles(page))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRendererLoadsEmbeddedPages(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"login", "databases", "error",
		"admin/users", "admin/groups", "admin/grants", "admin/connections",
		"explorer/schema", "explorer/table", "explorer/rows",
		"migration/list", "migration/detail", "migration/editor",
	} {
		w := httptest.NewRecorder()
		if err := r.Render(w, httptest.NewRequest("GET", "/", nil), http.StatusOK, name, Page{Title: "Judul"}); err != nil || w.Code != http.StatusOK {
			t.Fatal("embedded page not rendered: " + name)
		}
		if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "/static/css/app.css") {
			t.Fatal("wrong headers or missing layout: " + name)
		}
	}
}

func TestBrandTemplatesUseStickerLogo(t *testing.T) {
	for _, name := range []string{
		"templates/pages/login.html",
		"templates/partials/sidebar.html",
	} {
		content, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		template := string(content)
		if !strings.Contains(template, `/static/img/stiker-if.png`) {
			t.Errorf("%s does not use the sticker logo", name)
		}
		if strings.Contains(template, `/static/img/logo.svg`) {
			t.Errorf("%s still uses the placeholder logo", name)
		}
		if !strings.Contains(template, `alt="IF Tidur, Besok Balik Nubes"`) {
			t.Errorf("%s does not provide the logo alternative text", name)
		}
	}
}

func TestRendererEscapesUntrustedContent(t *testing.T) {
	r := testRenderer(t, `{{define "content"}}{{.Content}}{{end}}`)
	w := httptest.NewRecorder()
	if err := r.Render(w, httptest.NewRequest("GET", "/", nil), http.StatusOK, "test", Page{Content: "<script>alert(1)</script>"}); err != nil {
		t.Fatal(err)
	}
	if body := w.Body.String(); strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("untrusted content was not escaped")
	}
}

func TestRendererTakesViewerOnlyFromContext(t *testing.T) {
	r := testRenderer(t, `{{define "content"}}{{end}}`)
	w := httptest.NewRecorder()
	page := Page{Identity: security.Identity{Username: "mallory"}, SignedIn: true, CSRF: security.NewSecret("forged")}
	if err := r.Render(w, httptest.NewRequest("GET", "/", nil), http.StatusOK, "test", page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "mallory") || strings.Contains(w.Body.String(), "forged") || strings.Contains(w.Body.String(), "|") {
		t.Fatal("handler-supplied identity trusted without session")
	}
	r.viewer = func(context.Context) viewer {
		return viewer{identity: security.Identity{Username: "budi"}, csrf: security.NewSecret(testCSRF), signedIn: true}
	}
	w = httptest.NewRecorder()
	if err := r.Render(w, httptest.NewRequest("GET", "/", nil), http.StatusOK, "test", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), "budi|"+testCSRF+"|") {
		t.Fatal("session identity or CSRF token missing")
	}
}

func TestPageRedactsCSRFToken(t *testing.T) {
	page := Page{CSRF: security.NewSecret(testCSRF)}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", page, page, page), testCSRF) {
		t.Fatal("CSRF token leaked through formatting")
	}
}

func TestRendererFailsClosed(t *testing.T) {
	r := testRenderer(t, `{{define "content"}}partial{{.Content.Missing}}{{end}}`)
	w := httptest.NewRecorder()
	if err := r.Render(w, httptest.NewRequest("GET", "/", nil), http.StatusOK, "missing", Page{}); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatal("unknown page accepted")
	}
	if w.Code != http.StatusInternalServerError || w.Body.String() != "Terjadi kesalahan\n" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unknown page did not fail safely")
	}
	w = httptest.NewRecorder()
	if err := r.Render(w, httptest.NewRequest("GET", "/", nil), http.StatusOK, "test", Page{Content: struct{}{}}); err == nil {
		t.Fatal("execution error ignored")
	}
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "partial") || strings.Contains(w.Body.String(), "<title>") {
		t.Fatal("partial page sent after execution error")
	}
}

func TestNewRendererRejectsInvalidTemplates(t *testing.T) {
	if _, err := newRenderer(testFiles(`{{define "content"}}{{end`)); err == nil {
		t.Fatal("broken template accepted")
	}
	files := testFiles(`{{define "content"}}{{end}}`)
	files["templates/layouts/base.html"] = &fstest.MapFile{Data: []byte(`{{define "other"}}{{end}}`)}
	if _, err := newRenderer(files); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatal("page without base layout accepted")
	}
	delete(files, "templates/pages/test.html")
	if _, err := newRenderer(files); err == nil {
		t.Fatal("renderer without pages accepted")
	}
}
