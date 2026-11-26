// Package spa menyajikan hasil build SPA dari web/dist yang ditanam ke dalam binary
package spa

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

//go:embed all:dist
var embedded embed.FS

const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
	cacheNever      = "no-store"
	indexName       = "index.html"
	assetsDir       = "assets"
	// Penanda di index.html yang diganti dengan nonce baru pada setiap respons
	noncePlaceholder = "__CSP_NONCE__"
)

// Path ini milik backend sehingga tidak pernah dijawab dengan halaman SPA
var reserved = []string{"api", "healthz", "metrics"}

const placeholder = `<!doctype html>
<html lang="id">
  <head>
    <meta charset="utf-8" />
    <title>Tocatdz</title>
  </head>
  <body>
    <h1>Antarmuka web belum dibangun</h1>
    <p>Jalankan npm run build:embed di folder web lalu bangun ulang server.</p>
  </body>
</html>
`

type asset struct {
	body        []byte
	contentType string
	etag        string
	cache       string
	// Halaman yang memuat penanda nonce dan sehingga header CSP-nya dibuat per respons
	nonced bool
}

type Handler struct {
	files   map[string]*asset
	index   *asset
	handler http.Handler
}

var (
	_ security.RouteRegistrar = (*Handler)(nil)
	_ http.Handler            = (*Handler)(nil)
)

// New memakai isi src/internal/spa/dist, dan folder yang kosong menghasilkan halaman pengganti sampai SPA dibangun
func New() (*Handler, error) {
	root, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, err
	}
	return newHandler(root)
}

func newHandler(root fs.FS) (*Handler, error) {
	files := make(map[string]*asset)
	err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || hidden(name) {
			return nil
		}
		body, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}
		files[name] = newAsset(name, body)
		return nil
	})
	if err != nil {
		return nil, err
	}
	h := &Handler{files: files, index: files[indexName]}
	h.handler = security.SecurityHeaders(http.HandlerFunc(h.serve))
	return h, nil
}

func newAsset(name string, body []byte) *asset {
	a := &asset{body: body, contentType: contentTypeFor(name, body), cache: cacheRevalidate, nonced: name == indexName && bytes.Contains(body, []byte(noncePlaceholder))}
	switch {
	case name == indexName:
		a.cache = cacheNever
	case strings.HasPrefix(name, assetsDir+"/"):
		a.cache = cacheImmutable
	}
	if a.cache != cacheNever {
		sum := sha256.Sum256(body)
		a.etag = `"` + hex.EncodeToString(sum[:8]) + `"`
	}
	return a
}

// Built bernilai false selama dist belum berisi index.html
func (h *Handler) Built() bool { return h.index != nil }

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Pola / lebih umum dari /api/v1 dan /healthz sehingga keduanya tetap dilayani pemiliknya
	mux.Handle("/", h)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTP(w, r)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		reject(w, http.StatusMethodNotAllowed)
		return
	}
	name, ok := assetName(r.URL.Path)
	if !ok {
		reject(w, http.StatusNotFound)
		return
	}
	if found, exists := h.files[name]; exists {
		h.write(w, r, found)
		return
	}
	if name != indexName && (name == assetsDir || strings.HasPrefix(name, assetsDir+"/") || hasAssetExtension(name)) {
		reject(w, http.StatusNotFound)
		return
	}
	h.fallback(w, r)
}

// Route milik client dijawab dengan index.html supaya React Router yang menentukan halamannya
func (h *Handler) fallback(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", cacheNever)
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(placeholder))
		}
		return
	}
	h.write(w, r, h.index)
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request, a *asset) {
	if a.nonced {
		h.writeNonced(w, r, a)
		return
	}
	header := w.Header()
	header.Set("Content-Type", a.contentType)
	header.Set("Cache-Control", a.cache)
	if a.etag != "" {
		header.Set("ETag", a.etag)
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.body))
}

// Editor SQL menyuntikkan tag style saat runtime sehingga halaman memperoleh nonce yang diizinkan style-src, dan nonce yang sama dibaca SPA dari meta tag
func (h *Handler) writeNonced(w http.ResponseWriter, r *http.Request, a *asset) {
	nonce, err := newNonce()
	if err != nil {
		reject(w, http.StatusInternalServerError)
		return
	}
	header := w.Header()
	header.Set("Content-Type", a.contentType)
	header.Set("Cache-Control", a.cache)
	header.Set("Content-Security-Policy", withStyleNonce(security.ContentSecurityPolicy, nonce))
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(bytes.ReplaceAll(a.body, []byte(noncePlaceholder), []byte(nonce))))
}

func newNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// Hanya style-src yang diperluas dan script-src tetap hanya 'self'
func withStyleNonce(policy, nonce string) string {
	return strings.Replace(policy, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1)
}

func reject(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", cacheNever)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(http.StatusText(status) + "\n"))
}

// Mengubah path URL menjadi nama file di dist dan menolak path milik backend, path tidak valid, serta file tersembunyi
func assetName(urlPath string) (string, bool) {
	if !strings.HasPrefix(urlPath, "/") {
		return "", false
	}
	name := strings.TrimPrefix(urlPath, "/")
	if name == "" {
		return indexName, true
	}
	name = strings.TrimSuffix(name, "/")
	if !fs.ValidPath(name) || hidden(name) {
		return "", false
	}
	for _, prefix := range reserved {
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return "", false
		}
	}
	return name, true
}

func hidden(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			return true
		}
	}
	return false
}
