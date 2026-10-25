package migration

import (
	"context"
	"strings"
	"unicode"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type MySQLValidator struct{}

func NewMySQLValidator() *MySQLValidator { return &MySQLValidator{} }

func (*MySQLValidator) Validate(ctx context.Context, sql security.Secret) error {
	if ctx.Err() != nil {
		return security.ErrUnavailable
	}
	if ValidateDraft(Draft{Title: "SQL", SQL: sql}) != nil {
		return security.ErrInvalidInput
	}
	statements, ok := mysqlStatements(sql.Reveal())
	if !ok || len(statements) == 0 || len(statements) > 1000 {
		return SQLRejection("syntax")
	}
	for _, statement := range statements {
		if ctx.Err() != nil {
			return security.ErrUnavailable
		}
		tokens := mysqlTokens(statement)
		if !validMySQLStatement(tokens) {
			return SQLRejection("unsupported")
		}
	}
	return nil
}

func mysqlStatements(source string) ([]string, bool) {
	const (
		code = iota
		singleQuote
		doubleQuote
		backtick
		lineComment
		blockComment
	)
	state := code
	var current strings.Builder
	var result []string
	flush := func() {
		if text := strings.TrimSpace(current.String()); text != "" {
			result = append(result, text)
		}
		current.Reset()
	}
	for i := 0; i < len(source); i++ {
		ch := source[i]
		switch state {
		case code:
			switch {
			case ch == '\x00':
				return nil, false
			case ch == '\'':
				state = singleQuote
				current.WriteByte(' ')
			case ch == '"':
				state = doubleQuote
				current.WriteByte(' ')
			case ch == '`':
				state = backtick
				current.WriteByte(' ')
			case ch == '#':
				state = lineComment
				current.WriteByte(' ')
			case ch == '-' && i+2 < len(source) && source[i+1] == '-' && unicode.IsSpace(rune(source[i+2])):
				state = lineComment
				i++
				current.WriteByte(' ')
			case ch == '/' && i+1 < len(source) && source[i+1] == '*':
				if i+2 < len(source) && source[i+2] == '!' {
					return nil, false
				}
				state = blockComment
				i++
				current.WriteByte(' ')
			case ch == ';':
				flush()
			default:
				current.WriteByte(ch)
			}
		case singleQuote, doubleQuote:
			quote := byte('\'')
			if state == doubleQuote {
				quote = '"'
			}
			if ch == '\\' && i+1 < len(source) {
				i++
				continue
			}
			if ch == quote {
				if i+1 < len(source) && source[i+1] == quote {
					i++
					continue
				}
				state = code
			}
		case backtick:
			if ch == '`' {
				if i+1 < len(source) && source[i+1] == '`' {
					current.WriteByte('`')
					i++
					continue
				}
				state = code
				current.WriteByte(' ')
			} else {
				current.WriteByte(ch)
			}
		case lineComment:
			if ch == '\n' || ch == '\r' {
				state = code
				current.WriteByte(' ')
			}
		case blockComment:
			if ch == '*' && i+1 < len(source) && source[i+1] == '/' {
				state = code
				i++
				current.WriteByte(' ')
			}
		}
	}
	if state != code && state != lineComment {
		return nil, false
	}
	flush()
	return result, true
}

func mysqlTokens(statement string) []string {
	return strings.FieldsFunc(strings.ToLower(statement), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}

func validMySQLStatement(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	protected := map[string]bool{
		"_tocatdz_migration_receipts": true,
		"information_schema":          true,
		"performance_schema":          true,
		"mysql":                       true,
		"sys":                         true,
	}
	for _, token := range tokens {
		if protected[token] {
			return false
		}
	}
	forbidden := []string{"begin", "commit", "rollback", "savepoint", "autocommit", "delimiter", "prepare", "execute", "deallocate", "outfile", "infile", "handler", "lock", "unlock", "grant", "revoke", "call"}
	for _, token := range tokens {
		for _, denied := range forbidden {
			if token == denied {
				return false
			}
		}
	}
	if tokens[0] == "insert" || tokens[0] == "update" || tokens[0] == "delete" {
		return true
	}
	if len(tokens) < 2 {
		return false
	}
	allowedObjects := map[string]bool{"table": true, "index": true, "view": true}
	switch tokens[0] {
	case "create", "drop":
		if allowedObjects[tokens[1]] {
			return true
		}
		return tokens[0] == "create" && len(tokens) > 2 && tokens[1] == "unique" && tokens[2] == "index"
	case "alter", "rename", "truncate":
		return tokens[1] == "table"
	default:
		return false
	}
}
