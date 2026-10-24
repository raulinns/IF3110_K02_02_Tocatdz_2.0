package ui

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const maxIdentifierBytes = 63

func pathID(r *http.Request, name string) (security.ID, error) {
	id := security.ID(r.PathValue(name))
	if !security.ValidID(id) {
		return "", security.ErrInvalidInput
	}
	return id, nil
}

// Hanya bentuk nama yang dicek; keberadaannya divalidasi terhadap katalog database target
func pathIdentifier(r *http.Request, name string) (string, error) {
	value := r.PathValue(name)
	if value == "" || len(value) > maxIdentifierBytes || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", security.ErrInvalidInput
	}
	return value, nil
}

// Setiap segmen di-escape supaya nama schema/table seperti "a/b" tetap jadi satu segmen URL
func databasePath(databaseID security.ID, parts ...string) string {
	segments := []string{"", "databases", url.PathEscape(string(databaseID))}
	for _, part := range parts {
		segments = append(segments, url.PathEscape(part))
	}
	return strings.Join(segments, "/")
}

type DatabaseTabs struct {
	SchemaHref     string
	MigrationsHref string
	Active         string
}

func databaseTabs(databaseID security.ID, active string) DatabaseTabs {
	return DatabaseTabs{SchemaHref: databasePath(databaseID, "schema"), MigrationsHref: databasePath(databaseID, "migrations"), Active: active}
}
