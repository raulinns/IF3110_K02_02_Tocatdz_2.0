package migration

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	pgquery "github.com/wasilibs/go-pgquery/parser"
)

const (
	redactedStatement         = "[REDACTED SQL]"
	maxRedactedStatementBytes = 16 << 10
)

var (
	sqlPlaceholder    = regexp.MustCompile(`\$[1-9][0-9]*`)
	sqlLiteralKeyword = regexp.MustCompile(`(?i)\b(?:true|false|null)\b`)
)

// RedactStatement uses PostgreSQL normalization to replace literals. Some
// utility statements and comments survive normalization, so ensure no such
// fragments appear in the output before it can leave the history endpoint.
func RedactStatement(statement security.Secret) string {
	sql := statement.Reveal()
	if len(sql) == 0 || len(sql) > MaxSQLBytes || !utf8.ValidString(sql) || strings.ContainsRune(sql, 0) {
		return redactedStatement
	}
	normalized, err := pgquery.Normalize(sql)
	if err != nil || strings.TrimSpace(normalized) == "" || len(normalized) > maxRedactedStatementBytes ||
		strings.Contains(normalized, "--") || strings.Contains(normalized, "/*") {
		return redactedStatement
	}
	// Reject residual string, dollar-quote and numeric tokens. A digit inside
	// an identifier can also trigger this fallback, which is safe for display.
	check := sqlPlaceholder.ReplaceAllString(normalized, "")
	if strings.ContainsAny(check, "'$0123456789") || sqlLiteralKeyword.MatchString(check) {
		return redactedStatement
	}
	return normalized
}
