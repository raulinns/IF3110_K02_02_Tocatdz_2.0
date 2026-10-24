package explorer

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func validIdentifier(name string) bool {
	return len(name) > 0 && len(name) <= 63 && utf8.ValidString(name) && !strings.ContainsRune(name, 0)
}

func IsVisibleSchema(name string) bool {
	return validIdentifier(name) && name != "information_schema" && !strings.HasPrefix(name, "pg_")
}

// Identifier tidak bisa dijadikan parameter SQL, jadi tiap nama harus di-quote secara terpisah setelah cocok dengan catalog.
func QuoteIdentifier(name string) (string, error) {
	if !validIdentifier(name) {
		return "", security.ErrInvalidInput
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
}

func normalizePagination(page, limit int) (int, int, int64, error) {
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	if page < 1 || limit < 1 || limit > MaxLimit || int64(page-1) > MaxOffset/int64(limit) {
		return 0, 0, 0, security.ErrInvalidInput
	}
	return page, limit, int64(page-1) * int64(limit), nil
}

func ParseRowsQuery(schema, table string, values url.Values) (RowsQuery, error) {
	if !validIdentifier(schema) || !validIdentifier(table) {
		return RowsQuery{}, security.ErrInvalidInput
	}
	query := RowsQuery{Schema: schema, Table: table}
	for key, target := range map[string]*int{"page": &query.Page, "limit": &query.Limit} {
		items, exists := values[key]
		if !exists {
			continue
		}
		if len(items) != 1 {
			return RowsQuery{}, security.ErrInvalidInput
		}
		value, err := strconv.Atoi(items[0])
		if err != nil || value <= 0 || strconv.Itoa(value) != items[0] {
			return RowsQuery{}, security.ErrInvalidInput
		}
		*target = value
	}
	page, limit, _, err := normalizePagination(query.Page, query.Limit)
	if err != nil {
		return RowsQuery{}, err
	}
	query.Page, query.Limit = page, limit
	return query, nil
}
