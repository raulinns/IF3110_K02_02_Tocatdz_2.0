package ui

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Body form sudah diparse middleware saat memeriksa CSRF; data dari query URL tidak pernah dipakai
func readForm(r *http.Request, fields ...string) (url.Values, error) {
	if r.URL.RawQuery != "" || r.PostForm == nil {
		return nil, security.ErrInvalidInput
	}
	allowed := map[string]bool{security.CSRFFieldName: true}
	for _, field := range fields {
		allowed[field] = true
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			return nil, security.ErrInvalidInput
		}
	}
	return r.PostForm, nil
}

func formID(values url.Values, field string) (security.ID, error) {
	id := security.ID(values.Get(field))
	if !security.ValidID(id) {
		return "", security.ErrInvalidInput
	}
	return id, nil
}

func formRevision(values url.Values) (uint64, error) {
	revision, err := strconv.ParseUint(values.Get("revision"), 10, 64)
	if err != nil || revision == 0 {
		return 0, security.ErrInvalidInput
	}
	return revision, nil
}

func formText(values url.Values, field string, maxRunes int) (string, bool) {
	value := strings.TrimSpace(values.Get(field))
	return value, value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes
}
