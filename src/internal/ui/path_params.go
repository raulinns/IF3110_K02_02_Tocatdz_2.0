package ui

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
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
	DiffHref       string
	SchemaDiff     bool
	Active         string
}

func databaseCapabilities(ctx context.Context, source any, databaseID security.ID) (*dbms.Capabilities, error) {
	capabilities := dbms.Features(dbms.PostgreSQL)
	resolver, ok := source.(dbms.CapabilityResolver)
	if !ok {
		return &capabilities, nil
	}
	resolved, err := resolver.Capabilities(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

func databaseTabs(databaseID security.ID, active string, capabilities *dbms.Capabilities) DatabaseTabs {
	showDiff := true
	if capabilities != nil {
		showDiff = capabilities.SchemaDiff
	}
	return DatabaseTabs{
		SchemaHref: databasePath(databaseID, "schema"), MigrationsHref: databasePath(databaseID, "migrations"),
		DiffHref: databasePath(databaseID, "schema-diff"), SchemaDiff: showDiff, Active: active,
	}
}
