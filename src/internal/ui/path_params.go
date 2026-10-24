package ui

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

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
	if _, err := explorer.QuoteIdentifier(value); err != nil {
		return "", security.ErrInvalidInput
	}
	return value, nil
}

// Setiap segmen di-escape supaya nama schema/table seperti "a/b" tetap jadi satu segmen URL
func databasePath(databaseID security.ID, parts ...string) string {
	segments := []string{"", "databases", url.PathEscape(string(databaseID))}
	for _, part := range parts {
		escaped := url.PathEscape(part)
		if part == "." || part == ".." {
			escaped = strings.ReplaceAll(part, ".", "%2E")
		}
		segments = append(segments, escaped)
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
