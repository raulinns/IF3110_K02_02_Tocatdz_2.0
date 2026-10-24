package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrationUIRedactsSQLWithoutChangingRendering(t *testing.T) {
	const sql = "SELECT 'SQL_PRIVATE_CANARY';"
	draft := MigrationDraft{Title: "test", SQL: sql}
	form := MigrationForm{SQL: sql}
	detail := MigrationDetail{SQL: sql}
	values := []any{draft, &draft, form, &form, detail, &detail, Page{Content: form}, Page{Content: detail}}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%q", "%x"} {
			if strings.Contains(fmt.Sprintf(format, value), "SQL_PRIVATE_CANARY") {
				t.Fatal("UI formatting leaked SQL")
			}
		}
		var logs bytes.Buffer
		slog.New(slog.NewJSONHandler(&logs, nil)).Info("audit", "value", value)
		if strings.Contains(logs.String(), "SQL_PRIVATE_CANARY") {
			t.Fatal("UI log leaked SQL")
		}
	}
	for _, value := range []any{draft, form, detail} {
		encoded, err := json.Marshal(value)
		if err != nil || strings.Contains(string(encoded), "SQL_PRIVATE_CANARY") {
			t.Fatal("UI JSON unexpectedly exposed SQL")
		}
	}
	r := testRenderer(t, `{{define "content"}}{{.Content.SQL}}{{end}}`)
	w := httptest.NewRecorder()
	if err := r.Render(w, httptest.NewRequest("GET", "/", nil), 200, "test", Page{Content: detail}); err != nil || !strings.Contains(w.Body.String(), "SQL_PRIVATE_CANARY") {
		t.Fatal("redaction changed the authorized SQL view")
	}
}
