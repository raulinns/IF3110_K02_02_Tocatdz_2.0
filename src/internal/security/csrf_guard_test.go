package security

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestCSRFBindingAndEpoch(t *testing.T) {
	g, err := NewHMACCSRFGuard("https://platform.example", testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p := testPrincipal()
	token, err := g.Token(ctx, p)
	if err != nil || g.VerifyToken(ctx, p, token) != nil {
		t.Fatal("valid CSRF rejected")
	}
	if token.Reveal() == testToken(1).Reveal() {
		t.Fatal("CSRF must not be raw cookie/key")
	}
	for _, changed := range []Principal{
		{UserID: p.UserID, SessionID: otherID, AuthVersion: 1, CSRFEpoch: 1},
		{UserID: p.UserID, SessionID: p.SessionID, AuthVersion: 2, CSRFEpoch: 2},
	} {
		if err := g.VerifyToken(ctx, changed, token); !errors.Is(err, ErrCSRF) {
			t.Fatal("wrong family/epoch accepted")
		}
	}
	again, _ := g.Token(ctx, p)
	if again.Reveal() != token.Reveal() {
		t.Fatal("CSRF changed without an epoch transition")
	}
	other, _ := NewHMACCSRFGuard("https://platform.example", testKey(2))
	if !errors.Is(other.VerifyToken(ctx, p, token), ErrCSRF) {
		t.Fatal("wrong CSRF key accepted")
	}
	for _, bad := range []Secret{{}, NewSecret("bad"), testToken(9)} {
		if !errors.Is(g.VerifyToken(ctx, p, bad), ErrCSRF) {
			t.Fatal("incorrect CSRF accepted")
		}
	}
	if _, err := g.Token(ctx, Principal{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("untrusted empty principal accepted")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := g.Token(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatal("CSRF ignored cancellation")
	}
}

func TestOriginVerification(t *testing.T) {
	g, _ := NewHMACCSRFGuard("https://platform.example", testKey(1))
	tests := []struct {
		name, origin, referer, metadata string
		want                            bool
	}{
		{"origin", "https://platform.example", "", "same-origin", true},
		{"default port", "https://PLATFORM.example:443", "", "", true},
		{"referer fallback", "", "https://platform.example/form?tab=1", "", true},
		{"no headers", "", "", "", false},
		{"null", "null", "https://platform.example/form", "", false},
		{"foreign origin", "https://attacker.example", "https://platform.example/form", "", false},
		{"sibling site", "https://other.platform.example", "", "same-site", false},
		{"wrong port", "https://platform.example:444", "", "", false},
		{"wrong scheme", "http://platform.example", "", "", false},
		{"origin with path", "https://platform.example/form", "", "", false},
		{"origin with credentials", "https://user:pass@platform.example", "", "", false},
		{"origin list", "https://platform.example https://attacker.example", "", "", false},
		{"untrusted referer", "", "https://platform.example.attacker.example/form", "", false},
		{"cross-site metadata", "https://platform.example", "", "cross-site", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://attacker.example/login", nil)
			r.Header.Set("X-Forwarded-Host", "platform.example")
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				r.Header.Set("Referer", tt.referer)
			}
			if tt.metadata != "" {
				r.Header.Set("Sec-Fetch-Site", tt.metadata)
			}
			err := g.VerifyOrigin(r)
			if (err == nil) != tt.want || err != nil && !errors.Is(err, ErrCSRF) {
				t.Fatalf("Origin result %v, want valid=%v", err, tt.want)
			}
		})
	}
	for _, header := range []string{"Origin", "Referer", "Sec-Fetch-Site"} {
		r := httptest.NewRequest("POST", "/", nil)
		if header != "Referer" {
			r.Header.Set("Origin", "https://platform.example")
		}
		r.Header[header] = []string{"https://platform.example", "https://platform.example"}
		if !errors.Is(g.VerifyOrigin(r), ErrCSRF) {
			t.Fatal("duplicate security header accepted")
		}
	}
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		if err := g.VerifyOrigin(httptest.NewRequest(method, "/", nil)); err != nil {
			t.Fatal("safe method rejected")
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "CUSTOM"} {
		if err := g.VerifyOrigin(httptest.NewRequest(method, "/", nil)); !errors.Is(err, ErrCSRF) {
			t.Fatal("unsafe method unguarded")
		}
	}
}

func TestOriginConfiguration(t *testing.T) {
	for _, value := range []string{"https://platform.example", "http://localhost:8080", "https://[::1]:8443"} {
		if _, err := NewHMACCSRFGuard(value, testKey(1)); err != nil {
			t.Fatal("valid origin rejected")
		}
	}
	for _, value := range []string{"", "null", "//platform.example", "ftp://platform.example", "https://platform.example/", "https://platform.example:", "https://platform.example:0", "https://platform.example:65536", "https://platform.example?", "https://platform.example#x", "https://user@platform.example", "https://bad_host", " https://platform.example", "https://platform.example\\@attacker.example"} {
		if _, err := NewHMACCSRFGuard(value, testKey(1)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("malformed origin accepted: %q", value)
		}
	}
	if _, err := NewHMACCSRFGuard("https://platform.example", Secret{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing key accepted")
	}
	var zero HMACCSRFGuard
	if err := zero.VerifyOrigin(httptest.NewRequest("GET", "/", nil)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unconfigured guard accepted")
	}
}

func TestCSRFExtractionAndBodyReuse(t *testing.T) {
	token := testToken(1).Reveal()
	form := url.Values{CSRFFieldName: {token}, "sql": {"CREATE TABLE example(id int)"}}
	r := httptest.NewRequest("POST", "/save?tab=editor", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	got, err := ReadCSRFToken(httptest.NewRecorder(), r)
	if err != nil || got.Reveal() != token || r.PostForm.Get("sql") != form.Get("sql") {
		t.Fatal("form extraction lost handler fields")
	}
	body := `{"sql":"SELECT 1"}`
	r = httptest.NewRequest("POST", "/save", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(CSRFHeaderName, token)
	got, err = ReadCSRFToken(httptest.NewRecorder(), r)
	remaining, _ := io.ReadAll(r.Body)
	if err != nil || got.Reveal() != token || string(remaining) != body {
		t.Fatal("JSON body consumed by CSRF extractor")
	}
}

func TestCSRFFormSupportsAllUnsafeMethods(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "CUSTOM"} {
		body := url.Values{CSRFFieldName: {testToken(1).Reveal()}, "action": {"save"}}.Encode()
		r := httptest.NewRequest(method, "/?tab=editor", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, err := ReadCSRFToken(httptest.NewRecorder(), r); err != nil {
			t.Fatalf("valid %s form rejected: %v", method, err)
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("action") != "save" || r.Form.Get("tab") != "editor" {
			t.Fatalf("handler fields lost for %s", method)
		}
	}
}

func TestCSRFExtractionRejectsPollution(t *testing.T) {
	token := testToken(1).Reveal()
	tests := []struct {
		name, path, body, media string
		headers                 []string
	}{
		{"missing", "/", "x=1", "application/x-www-form-urlencoded", nil},
		{"query only", "/?csrf_token=" + token, "", "", nil},
		{"query and header", "/?csrf_token=" + token, "", "", []string{token}},
		{"duplicate fields", "/", "csrf_token=" + token + "&csrf_token=" + token, "application/x-www-form-urlencoded", nil},
		{"form and header", "/", "csrf_token=" + token, "application/x-www-form-urlencoded", []string{token}},
		{"duplicate headers", "/", "", "", []string{token, token}},
		{"malformed token", "/", "csrf_token=bad", "application/x-www-form-urlencoded", nil},
		{"invalid form encoding", "/", "csrf_token=%zz", "application/x-www-form-urlencoded", nil},
		{"empty header", "/", "", "", []string{""}},
		{"multipart", "/", "", "multipart/form-data; boundary=example", []string{token}},
		{"malformed media", "/", "", "application/json; charset=\"", []string{token}},
		{"oversized", "/", "csrf_token=" + token + "&sql=" + strings.Repeat("x", int(MaxCSRFFormBytes)), "application/x-www-form-urlencoded", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tt.path, strings.NewReader(tt.body))
			if tt.media != "" {
				r.Header.Set("Content-Type", tt.media)
			}
			for _, value := range tt.headers {
				r.Header.Add(CSRFHeaderName, value)
			}
			if got, err := ReadCSRFToken(httptest.NewRecorder(), r); !errors.Is(err, ErrCSRF) || !got.Empty() {
				t.Fatal("ambiguous/missing/oversized CSRF accepted")
			}
		})
	}
}

func TestCSRFConcurrentUse(t *testing.T) {
	g, _ := NewHMACCSRFGuard("https://platform.example", testKey(1))
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := g.Token(context.Background(), testPrincipal())
			if err != nil || g.VerifyToken(context.Background(), testPrincipal(), token) != nil {
				t.Error("concurrent CSRF verification failed")
			}
		}()
	}
	wg.Wait()
}
