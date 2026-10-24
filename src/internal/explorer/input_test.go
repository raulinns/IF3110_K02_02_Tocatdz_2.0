package explorer

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestQuotedIdentifiersPreserveNamesWithoutAllowingSQLBreakout(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"items", `"items"`}, {"Mixed Case", `"Mixed Case"`}, {"odd.name", `"odd.name"`}, {`x"; DROP TABLE users;--`, `"x""; DROP TABLE users;--"`}, {"名前", `"名前"`},
		{"line\nbreak", "\"line\nbreak\""},
	} {
		got, err := QuoteIdentifier(test.input)
		if err != nil || got != test.want {
			t.Fatal("identifier was modified or not quoted as one identifier")
		}
	}
	for _, input := range []string{"", strings.Repeat("x", 64), "bad\x00name", string([]byte{0xff})} {
		if _, err := QuoteIdentifier(input); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("invalid identifier accepted")
		}
	}
}

func TestParseRowsQueryDefaultsLimitsAndOverflow(t *testing.T) {
	query, err := ParseRowsQuery("public", "items", nil)
	if err != nil || query.Page != 1 || query.Limit != 50 {
		t.Fatal("missing pagination defaults")
	}
	query, err = ParseRowsQuery("public", "items", url.Values{"page": {"2"}, "limit": {"200"}})
	if err != nil || query.Page != 2 || query.Limit != 200 {
		t.Fatal("valid pagination rejected")
	}
	for _, values := range []url.Values{
		{"page": {"0"}}, {"page": {"-1"}}, {"page": {"1", "2"}}, {"limit": {"201"}}, {"limit": {"0"}},
		{"limit": {"50", "50"}}, {"page": {"9999999999999999999999999999999"}}, {"page": {"1000001"}, "limit": {"200"}}, {"page": {"01"}}, {"limit": {""}},
	} {
		if _, err := ParseRowsQuery("public", "items", values); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("invalid pagination accepted")
		}
	}
}

func TestSystemSchemasAreHiddenButQuotedUserSchemasRemainVisible(t *testing.T) {
	for _, name := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_99"} {
		if IsVisibleSchema(name) {
			t.Fatal("internal schema visible")
		}
	}
	for _, name := range []string{"public", "sales", "Mixed Case", "PG_custom"} {
		if !IsVisibleSchema(name) {
			t.Fatal("user schema incorrectly hidden")
		}
	}
}
