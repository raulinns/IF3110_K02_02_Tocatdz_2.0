package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStickerLogoIsServedAsPNG(t *testing.T) {
	static, err := NewStaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	static.RegisterRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/static/img/stiker-if.png", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("sticker logo status = %d, want %d", w.Code, http.StatusOK)
	}
	if contentType := w.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("sticker logo content type = %q, want image/png", contentType)
	}
	if !strings.HasPrefix(w.Body.String(), "\x89PNG\r\n\x1a\n") {
		t.Fatal("sticker logo is not a PNG file")
	}
}

func TestStaticAssetsServeOnlyFiles(t *testing.T) {
	static, err := NewStaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	static.RegisterRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/static/css/app.css", nil))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Fatal("stylesheet not served")
	}
	for _, target := range []string{"/static/", "/static/css", "/static/css/", "/static/missing.css"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != http.StatusNotFound {
			t.Fatal("directory or missing file exposed: " + target)
		}
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/static/css/app.css", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("static assets accepted unsafe method")
	}
}
