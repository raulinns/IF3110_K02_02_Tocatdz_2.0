package migration

import (
	"context"
	"net/url"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func FuzzHistoryQuery(f *testing.F) {
	for _, seed := range []string{"", "page=1&limit=50", "page=01", "page=1&page=2", "limit=9999999999999999999999"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			return
		}
		values, err := url.ParseQuery(raw)
		if err != nil {
			return
		}
		query, err := ParseHistoryQuery(values)
		if err == nil && (query.Page < 1 || query.Limit < 1 || query.Limit > MaxLimit || int64(query.Page-1) > MaxOffset/int64(query.Limit)) {
			t.Fatal("accepted invalid pagination")
		}
	})
}
func FuzzSQLValidator(f *testing.F) {
	for _, seed := range []string{"SELECT 1;", "BEGIN;", "CREATE TABLE public.t(id int)", "SELECT ';COMMIT'", "/*nested /* comment */ */ SELECT 1"} {
		f.Add(seed)
	}
	v := NewPostgreSQLValidator()
	f.Fuzz(func(t *testing.T, sql string) {
		if len(sql) > 1024 {
			return
		}
		_ = v.Validate(context.Background(), security.NewSecret(sql))
	})
}
