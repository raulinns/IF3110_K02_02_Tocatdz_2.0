package bootstrap

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestQueryPlanHTTPWithRealPostgreSQL(t *testing.T) {
	e := newE2E(t)
	server := httptest.NewServer(e.handler)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func(method, path string, form url.Values, want int) (string, string) {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		r, err := http.NewRequest(method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if method == "POST" {
			r.Header.Set("Origin", origin)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d", method, path, response.StatusCode, want)
		}
		return string(raw), response.Header.Get("Location")
	}
	do("POST", "/login", url.Values{"username": {"admin"}, "password": {adminPassword}}, 303)
	listing, _ := do("GET", "/databases", nil, 200)
	token := match(t, csrfField, listing)
	base := "/databases/" + match(t, databaseLink, listing)
	_, detail := do("POST", base+"/migrations", url.Values{"csrf_token": {token}, "title": {"Plan SELECT"}, "sql": {"SELECT * FROM public.pelanggan WHERE id=1;"}}, 303)
	page, _ := do("GET", detail, nil, 200)
	if !strings.Contains(page, "Nomor statement") {
		t.Fatal("analysis form missing")
	}
	form := url.Values{"csrf_token": {token}, "revision": {"1"}, "statement": {"1"}, "mode": {"explain"}}
	plan, _ := do("POST", detail+"/plan", form, 200)
	if !strings.Contains(plan, "Estimated rows") || !strings.Contains(plan, "public.pelanggan") || !strings.Contains(plan, "JSON plan terstruktur") {
		t.Fatal("estimated plan not rendered")
	}
	form.Set("mode", "analyze")
	plan, _ = do("POST", detail+"/plan", form, 200)
	if !strings.Contains(plan, "EXPLAIN ANALYZE") || !strings.Contains(plan, "Execution Time") || !strings.Contains(plan, "Actual Rows") {
		t.Fatal("actual plan metrics missing")
	}
	page, _ = do("GET", detail, nil, 200)
	if !strings.Contains(page, "Draft") || match(t, revisionField, page) != "1" {
		t.Fatal("analysis changed draft")
	}
	form.Set("revision", "2")
	do("POST", detail+"/plan", form, 409)
	form.Set("revision", "1")
	form.Set("sql", "DELETE FROM public.pelanggan;")
	do("POST", detail+"/plan", form, 400)
	form.Del("sql")
	form.Del("csrf_token")
	do("POST", detail+"/plan", form, 403)
	form.Set("csrf_token", token)
	form.Set("statement", "999")
	do("POST", detail+"/plan", form, 400)
	form.Set("statement", "1")
	_, dml := do("POST", base+"/migrations", url.Values{"csrf_token": {token}, "title": {"Plan UPDATE"}, "sql": {"UPDATE public.pelanggan SET nama='MUTATION_CANARY' WHERE id=1;"}}, 303)
	form.Set("mode", "explain")
	do("POST", dml+"/plan", form, 200)
	form.Set("mode", "analyze")
	rejected, _ := do("POST", dml+"/plan", form, 400)
	if !strings.Contains(rejected, "SELECT baca saja") {
		t.Fatal("unsafe ANALYZE missing explanation")
	}
	var name string
	if err := e.target.QueryRow(t.Context(), "SELECT nama FROM public.pelanggan WHERE id=1").Scan(&name); err != nil || name != "Budi" {
		t.Fatal("analysis changed data")
	}
	do("POST", "/login", url.Values{"username": {"pengamat"}, "password": {viewerPassword}}, 303)
	viewerPage, _ := do("GET", detail, nil, 200)
	if strings.Contains(viewerPage, "Nomor statement") {
		t.Fatal("viewer sees analysis form")
	}
	form.Set("csrf_token", match(t, csrfField, viewerPage))
	do("POST", detail+"/plan", form, 403)
}
