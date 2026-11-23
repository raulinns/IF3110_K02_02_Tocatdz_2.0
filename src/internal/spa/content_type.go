package spa

import (
	"mime"
	"net/http"
	"path"
	"strings"
)

// Tipe konten ditetapkan di sini karena registry Windows bisa memetakan .js ke tipe yang salah dan nosniff menolak skrip seperti itu
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".otf":         "font/otf",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "application/xml",
	".webmanifest": "application/manifest+json",
}

func contentTypeFor(name string, body []byte) string {
	ext := strings.ToLower(path.Ext(name))
	if kind, ok := contentTypes[ext]; ok {
		return kind
	}
	if kind := mime.TypeByExtension(ext); kind != "" {
		return kind
	}
	return http.DetectContentType(body)
}

// Path dengan ekstensi file yang dikenal dianggap permintaan aset sehingga tidak boleh dijawab dengan index.html
func hasAssetExtension(name string) bool {
	_, ok := contentTypes[strings.ToLower(path.Ext(name))]
	return ok
}
