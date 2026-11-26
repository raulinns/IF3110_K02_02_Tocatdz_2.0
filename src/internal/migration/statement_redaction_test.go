package migration

import (
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestRedactStatementUsesPostgreSQLTokens(t *testing.T) {
	raw := `SELECT $1, 987, 1.25, 'private string', $body$private body$body$`
	got := RedactStatement(security.NewSecret(raw))
	if got != "SELECT $1, $2, $3, $4, $5" ||
		strings.Contains(got, "private") || strings.Contains(got, "987") {
		t.Fatalf("unexpected redaction: %s", got)
	}
	if got := RedactStatement(security.NewSecret("SELECT 'unterminated")); got != redactedStatement {
		t.Fatal("invalid quoting must fail closed")
	}
	if got := RedactStatement(security.NewSecret("SELECT 1 /* private comment */")); got != redactedStatement {
		t.Fatal("comment text must fail closed")
	}
}
