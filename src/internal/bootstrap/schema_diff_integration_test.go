package bootstrap

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var sqlTextarea = regexp.MustCompile(`(?s)<textarea[^>]*name="sql"[^>]*>(.*?)</textarea>`)

func TestSchemaDiffDraftFlowWithRealPostgreSQL(t *testing.T) {
	e := newE2E(t)
	if _, err := e.target.Exec(t.Context(), `CREATE SCHEMA rancangan;
		CREATE TABLE rancangan.pelanggan (id serial PRIMARY KEY, nama text NOT NULL, email text, aktif boolean NOT NULL DEFAULT true);
		CREATE INDEX pelanggan_email_idx ON rancangan.pelanggan (lower(email));`); err != nil {
		t.Fatal(err)
	}
	cookie := e.login(t, "admin", adminPassword)
	listing := e.do("GET", "/databases", nil, cookie).Body.String()
	id := match(t, databaseLink, listing)
	base := "/databases/" + id
	diffURL := base + "/schema-diff?" + url.Values{"schema": {"public"}, "source": {id}, "source_schema": {"rancangan"}}.Encode()
	w := e.do("GET", diffURL, nil, cookie)
	page := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(page, "lolos validasi") || !strings.Contains(page, "pelanggan.aktif") {
		t.Fatalf("diff page incomplete: %d", w.Code)
	}
	sql := html.UnescapeString(match(t, sqlTextarea, page))
	if !strings.Contains(sql, "ADD COLUMN aktif boolean DEFAULT true NOT NULL") || !strings.Contains(sql, "pg_catalog.lower(email)") {
		t.Fatalf("unexpected candidate:\n%s", sql)
	}
	token := match(t, csrfField, page)
	edited := sql + "\nCOMMENT ON TABLE public.pelanggan IS 'ditinjau';"
	w = e.do("POST", base+"/migrations", url.Values{"csrf_token": {token}, "title": {"Sinkronkan rancangan"}, "sql": {edited}}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("draft from candidate rejected: %d", w.Code)
	}
	detail := w.Header().Get("Location")
	w = e.do("POST", detail+"/execute", url.Values{"csrf_token": {token}, "revision": {"1"}}, cookie)
	if w.Code != http.StatusSeeOther || !strings.Contains(e.do("GET", detail, nil, cookie).Body.String(), "Berhasil (commit)") {
		t.Fatalf("candidate migration not applied: %d", w.Code)
	}
	if page := e.do("GET", diffURL, nil, cookie).Body.String(); !strings.Contains(page, "Tidak ada perbedaan") {
		t.Fatal("schema still differs after applying candidate")
	}
	var count int
	if err := e.target.QueryRow(t.Context(), "SELECT count(*) FROM public.pelanggan WHERE aktif").Scan(&count); err != nil || count != 2 {
		t.Fatal("existing rows changed by migration")
	}
	if _, err := e.target.Exec(t.Context(), "ALTER TABLE rancangan.pelanggan ADD COLUMN catatan text"); err != nil {
		t.Fatal(err)
	}
	viewer := e.login(t, "pengamat", viewerPassword)
	w = e.do("GET", diffURL, nil, viewer)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `name="sql"`) || !strings.Contains(w.Body.String(), "Hanya baca") {
		t.Fatalf("view-only user got editable candidate: %d", w.Code)
	}
	if w := e.do("GET", base+"/schema-diff?schema=public&source="+id+"&source_schema=tidak_ada", nil, cookie); w.Code != http.StatusNotFound {
		t.Fatalf("missing schema: got %d", w.Code)
	}
}
