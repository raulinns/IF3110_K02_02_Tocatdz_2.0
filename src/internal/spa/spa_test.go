package spa

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
)

const indexHTML = `<!doctype html><html><head><script type="module" src="/assets/index-abc.js"></script><link rel="stylesheet" href="/assets/index-abc.css"></head><body><div id="root"></div></body></html>`

func testFiles() fstest.MapFS {
	return fstest.MapFS{
		"index.html":            {Data: []byte(indexHTML)},
		"theme.js":              {Data: []byte("document.documentElement.dataset.bsTheme = 'dark'")},
		"assets/index-abc.js":   {Data: []byte("console.log('app')")},
		"assets/index-abc.css":  {Data: []byte("body{margin:0}")},
		"assets/font-abc.woff2": {Data: []byte("woff2")},
		".gitkeep":              {Data: nil},
		"assets/.hidden":        {Data: []byte("rahasia")},
	}
}

func newTestHandler(t *testing.T, files fstest.MapFS) *Handler {
	t.Helper()
	handler, err := newHandler(files)
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	return handler
}

func serve(handler http.Handler, method, target string, headers ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func expectStatus(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d (body %q)", recorder.Code, status, recorder.Body.String())
	}
}

func expectHeader(t *testing.T, recorder *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := recorder.Header().Get(name); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}

func TestRootServesIndexWithSecurityHeaders(t *testing.T) {
	recorder := serve(newTestHandler(t, testFiles()), http.MethodGet, "/")
	expectStatus(t, recorder, http.StatusOK)
	if recorder.Body.String() != indexHTML {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	expectHeader(t, recorder, "Content-Type", "text/html; charset=utf-8")
	expectHeader(t, recorder, "Cache-Control", "no-store")
	expectHeader(t, recorder, "ETag", "")
	expectHeader(t, recorder, "Content-Security-Policy", security.ContentSecurityPolicy)
	expectHeader(t, recorder, "X-Content-Type-Options", "nosniff")
}

func TestClientRoutesFallBackToIndex(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	for _, target := range []string{
		"/login",
		"/databases",
		"/databases/",
		"/databases/abc/schema/public/orders",
		"/databases/abc/schema/public/tabel.v2",
		"/admin/users",
		"/apiary",
		"/index.html",
		"/dalam/sekali/sebuah/route/",
	} {
		t.Run(target, func(t *testing.T) {
			recorder := serve(handler, http.MethodGet, target)
			expectStatus(t, recorder, http.StatusOK)
			if recorder.Body.String() != indexHTML {
				t.Fatalf("body = %q", recorder.Body.String())
			}
			expectHeader(t, recorder, "Cache-Control", "no-store")
		})
	}
}

func TestQueryStringIsIgnoredForRouting(t *testing.T) {
	recorder := serve(newTestHandler(t, testFiles()), http.MethodGet, "/databases/abc/history?hide_rollback=true&page=2")
	expectStatus(t, recorder, http.StatusOK)
	if recorder.Body.String() != indexHTML {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestHashedAssetsAreCachedForever(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	for target, kind := range map[string]string{
		"/assets/index-abc.js":   "text/javascript; charset=utf-8",
		"/assets/index-abc.css":  "text/css; charset=utf-8",
		"/assets/font-abc.woff2": "font/woff2",
	} {
		t.Run(target, func(t *testing.T) {
			recorder := serve(handler, http.MethodGet, target)
			expectStatus(t, recorder, http.StatusOK)
			expectHeader(t, recorder, "Content-Type", kind)
			expectHeader(t, recorder, "Cache-Control", "public, max-age=31536000, immutable")
			if recorder.Header().Get("ETag") == "" {
				t.Fatal("ETag kosong")
			}
		})
	}
	recorder := serve(handler, http.MethodGet, "/assets/index-abc.js")
	if recorder.Body.String() != "console.log('app')" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestMissingAssetsAreNotFoundInsteadOfIndex(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	for _, target := range []string{
		"/assets/missing.js",
		"/assets/",
		"/assets",
		"/assets/deep/missing.css",
		"/favicon.ico",
		"/robots.txt",
		"/main.js.map",
		"/missing.woff2",
	} {
		t.Run(target, func(t *testing.T) {
			recorder := serve(handler, http.MethodGet, target)
			expectStatus(t, recorder, http.StatusNotFound)
			expectHeader(t, recorder, "Cache-Control", "no-store")
			expectHeader(t, recorder, "Content-Type", "text/plain; charset=utf-8")
			if strings.Contains(recorder.Body.String(), "<html") {
				t.Fatalf("aset yang hilang tidak boleh dijawab dengan HTML: %q", recorder.Body.String())
			}
		})
	}
}

func TestRootFilesRevalidateWithETag(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	first := serve(handler, http.MethodGet, "/theme.js")
	expectStatus(t, first, http.StatusOK)
	expectHeader(t, first, "Cache-Control", "no-cache")
	expectHeader(t, first, "Content-Type", "text/javascript; charset=utf-8")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag kosong")
	}
	second := serve(handler, http.MethodGet, "/theme.js", "If-None-Match", etag)
	expectStatus(t, second, http.StatusNotModified)
	if second.Body.Len() != 0 {
		t.Fatalf("304 tidak boleh membawa body: %q", second.Body.String())
	}
	changed := serve(handler, http.MethodGet, "/theme.js", "If-None-Match", `"lain"`)
	expectStatus(t, changed, http.StatusOK)
}

func TestETagFollowsContent(t *testing.T) {
	files := testFiles()
	before := newTestHandler(t, files).files["theme.js"].etag
	files["theme.js"] = &fstest.MapFile{Data: []byte("berubah")}
	after := newTestHandler(t, files).files["theme.js"].etag
	if before == after {
		t.Fatalf("ETag sama untuk isi yang berbeda: %s", before)
	}
}

func TestBackendPathsNeverServeTheSPA(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	for _, target := range []string{"/api", "/api/", "/api/v1", "/api/v1/databases", "/api/unknown", "/healthz", "/metrics", "/metrics/x"} {
		t.Run(target, func(t *testing.T) {
			recorder := serve(handler, http.MethodGet, target)
			expectStatus(t, recorder, http.StatusNotFound)
			if strings.Contains(recorder.Body.String(), "<html") {
				t.Fatalf("path backend dijawab dengan HTML: %q", recorder.Body.String())
			}
		})
	}
}

func TestOnlyGetAndHeadAreAllowed(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			recorder := serve(handler, method, "/")
			expectStatus(t, recorder, http.StatusMethodNotAllowed)
			expectHeader(t, recorder, "Allow", "GET, HEAD")
			expectHeader(t, recorder, "Cache-Control", "no-store")
		})
	}
}

func TestHeadReturnsHeadersWithoutBody(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	for _, target := range []string{"/", "/databases/abc", "/assets/index-abc.js"} {
		t.Run(target, func(t *testing.T) {
			recorder := serve(handler, http.MethodHead, target)
			expectStatus(t, recorder, http.StatusOK)
			if recorder.Body.Len() != 0 {
				t.Fatalf("HEAD membawa body: %q", recorder.Body.String())
			}
			if length, err := strconv.Atoi(recorder.Header().Get("Content-Length")); err != nil || length == 0 {
				t.Fatalf("Content-Length = %q", recorder.Header().Get("Content-Length"))
			}
		})
	}
}

func TestUnsafeAndHiddenPathsAreNotFound(t *testing.T) {
	handler := newTestHandler(t, testFiles())
	for _, target := range []string{
		"/../etc/passwd",
		"/assets/../index.html",
		"/a//b",
		"/.env",
		"/.git/config",
		"/.gitkeep",
		"/assets/.hidden",
	} {
		t.Run(target, func(t *testing.T) {
			recorder := serve(handler, http.MethodGet, target)
			expectStatus(t, recorder, http.StatusNotFound)
			if strings.Contains(recorder.Body.String(), "rahasia") {
				t.Fatal("file tersembunyi bocor")
			}
		})
	}
	if _, exists := newTestHandler(t, testFiles()).files[".gitkeep"]; exists {
		t.Fatal(".gitkeep tidak boleh ikut dimuat")
	}
}

func TestRangeRequestsAreSupportedForAssets(t *testing.T) {
	recorder := serve(newTestHandler(t, testFiles()), http.MethodGet, "/assets/index-abc.js", "Range", "bytes=0-6")
	expectStatus(t, recorder, http.StatusPartialContent)
	if recorder.Body.String() != "console" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	expectHeader(t, recorder, "Content-Range", "bytes 0-6/18")
}

func TestPlaceholderUntilTheSPAIsBuilt(t *testing.T) {
	handler := newTestHandler(t, fstest.MapFS{".gitkeep": {Data: nil}})
	if handler.Built() {
		t.Fatal("Built() seharusnya false tanpa index.html")
	}
	for _, target := range []string{"/", "/login", "/databases/abc"} {
		t.Run(target, func(t *testing.T) {
			recorder := serve(handler, http.MethodGet, target)
			expectStatus(t, recorder, http.StatusServiceUnavailable)
			expectHeader(t, recorder, "Cache-Control", "no-store")
			expectHeader(t, recorder, "Content-Type", "text/html; charset=utf-8")
			expectHeader(t, recorder, "Content-Security-Policy", security.ContentSecurityPolicy)
			if !strings.Contains(recorder.Body.String(), "belum dibangun") {
				t.Fatalf("body = %q", recorder.Body.String())
			}
		})
	}
	expectStatus(t, serve(handler, http.MethodGet, "/assets/index-abc.js"), http.StatusNotFound)
	head := serve(handler, http.MethodHead, "/")
	expectStatus(t, head, http.StatusServiceUnavailable)
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD membawa body: %q", head.Body.String())
	}
	if !newTestHandler(t, testFiles()).Built() {
		t.Fatal("Built() seharusnya true jika index.html ada")
	}
}

func TestContentTypes(t *testing.T) {
	cases := map[string]string{
		"index.html":           "text/html; charset=utf-8",
		"assets/app.js":        "text/javascript; charset=utf-8",
		"assets/app.mjs":       "text/javascript; charset=utf-8",
		"assets/APP.CSS":       "text/css; charset=utf-8",
		"assets/data.json":     "application/json",
		"assets/app.js.map":    "application/json",
		"logo.svg":             "image/svg+xml",
		"logo.png":             "image/png",
		"foto.jpeg":            "image/jpeg",
		"favicon.ico":          "image/x-icon",
		"assets/a.woff":        "font/woff",
		"assets/a.woff2":       "font/woff2",
		"manifest.webmanifest": "application/manifest+json",
	}
	for name, want := range cases {
		if got := contentTypeFor(name, nil); got != want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", name, got, want)
		}
	}
	if got := contentTypeFor("aneh.zzz-tidak-dikenal", []byte{0x00, 0x01, 0x02}); got != "application/octet-stream" {
		t.Errorf("tipe untuk ekstensi tidak dikenal = %q", got)
	}
}

type stubAPI struct{}

func (stubAPI) RegisterRoutes(mux *http.ServeMux) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"api":true}`)
	})
	mux.Handle("/api/v1", handler)
	mux.Handle("/api/v1/", handler)
}

// Pola yang sama dengan paket api didaftarkan bersama SPA agar konflik ServeMux terdeteksi di test
func TestRegistersNextToTheAPIAndHealthRoutes(t *testing.T) {
	router := web.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), stubAPI{}, newTestHandler(t, testFiles()))

	api := serve(router, http.MethodPost, "/api/v1/databases")
	expectStatus(t, api, http.StatusOK)
	if api.Body.String() != `{"api":true}` {
		t.Fatalf("POST /api/v1 tidak sampai ke API: %q", api.Body.String())
	}
	expectStatus(t, serve(router, http.MethodGet, "/api/v1"), http.StatusOK)

	health := serve(router, http.MethodGet, "/healthz")
	expectStatus(t, health, http.StatusOK)
	if !strings.Contains(health.Body.String(), `"ok"`) {
		t.Fatalf("healthz = %q", health.Body.String())
	}

	other := serve(router, http.MethodGet, "/api/lain")
	expectStatus(t, other, http.StatusNotFound)
	if strings.Contains(other.Body.String(), "<html") {
		t.Fatal("/api/lain dijawab dengan SPA")
	}

	for _, target := range []string{"/", "/login", "/databases/abc/history"} {
		page := serve(router, http.MethodGet, target)
		expectStatus(t, page, http.StatusOK)
		if page.Body.String() != indexHTML {
			t.Fatalf("%s tidak menyajikan index: %q", target, page.Body.String())
		}
		expectHeader(t, page, "Content-Security-Policy", security.ContentSecurityPolicy)
		if page.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s tidak melewati middleware router", target)
		}
	}
	expectStatus(t, serve(router, http.MethodPost, "/login"), http.StatusMethodNotAllowed)
}

var (
	scriptTag   = regexp.MustCompile(`(?is)<script\b[^>]*>`)
	inlineStyle = regexp.MustCompile(`(?i)<style\b|\sstyle\s*=|\son[a-z]+\s*=|javascript:`)
)

// Tag script tanpa atribut src berarti kode inline yang diblokir CSP
func inlineScripts(body string) []string {
	var found []string
	for _, tag := range scriptTag.FindAllString(body, -1) {
		if !strings.Contains(strings.ToLower(tag), "src=") {
			found = append(found, tag)
		}
	}
	return found
}

var assetReference = regexp.MustCompile(`(?i)(?:src|href)="(/[^"]+)"`)

// Setiap file yang dirujuk index.html hasil build harus benar-benar tersaji dengan tipe konten yang bukan HTML
func TestEmbeddedIndexReferencesServedFiles(t *testing.T) {
	handler, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !handler.Built() {
		t.Skip("SPA belum dibangun, jalankan npm run build:embed di folder web")
	}
	references := assetReference.FindAllStringSubmatch(serve(handler, http.MethodGet, "/").Body.String(), -1)
	if len(references) == 0 {
		t.Fatal("index.html tidak merujuk file apa pun")
	}
	for _, reference := range references {
		recorder := serve(handler, http.MethodGet, reference[1])
		expectStatus(t, recorder, http.StatusOK)
		if strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s dijawab dengan HTML", reference[1])
		}
	}
}

// Hasil build yang ditanam harus tetap lolos CSP script-src dan style-src 'self' dari paket security
func TestEmbeddedBuildIsServedAndCSPCompatible(t *testing.T) {
	handler, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recorder := serve(handler, http.MethodGet, "/")
	expectHeader(t, recorder, "Content-Security-Policy", security.ContentSecurityPolicy)
	if !handler.Built() {
		expectStatus(t, recorder, http.StatusServiceUnavailable)
		return
	}
	expectStatus(t, recorder, http.StatusOK)
	body := recorder.Body.String()
	if found := inlineScripts(body); len(found) > 0 {
		t.Errorf("index.html memuat <script> inline yang diblokir CSP: %v", found)
	}
	if inlineStyle.MatchString(body) {
		t.Errorf("index.html memuat style atau handler inline yang diblokir CSP: %s", body)
	}
	for name := range handler.files {
		if strings.HasPrefix(name, "assets/") && serve(handler, http.MethodGet, "/"+name).Header().Get("Cache-Control") != cacheImmutable {
			t.Errorf("aset %s tidak di-cache panjang", name)
		}
	}
}
