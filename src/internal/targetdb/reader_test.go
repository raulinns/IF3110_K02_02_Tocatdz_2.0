package targetdb

import (
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
)

func TestQuoteIdentifierUsesSelectedDialect(t *testing.T) {
	tests := []struct {
		engine dbms.Engine
		input  string
		want   string
	}{
		{engine: dbms.PostgreSQL, input: `order"items`, want: `"order""items"`},
	}
	for _, test := range tests {
		got, err := quoteIdentifier(test.engine, test.input)
		if err != nil || got != test.want {
			t.Fatalf("quoteIdentifier(%q, %q) = (%q, %v), want %q", test.engine, test.input, got, err, test.want)
		}
	}
}

func TestCellFromValuePreservesNullTextBinaryAndTime(t *testing.T) {
	now := time.Date(2026, time.October, 25, 8, 30, 0, 0, time.UTC)
	tests := []struct {
		name     string
		value    any
		dbType   string
		want     string
		null     bool
		encoding explorer.CellEncoding
	}{
		{name: "null", value: nil, null: true},
		{name: "text bytes", value: []byte("hello"), dbType: "VARCHAR", want: "hello", encoding: explorer.EncodingText},
		{name: "binary", value: []byte{0, 1, 2}, dbType: "BYTEA", want: `\x000102`, encoding: explorer.EncodingHex},
		{name: "time", value: now, want: "2026-10-25T08:30:00Z", encoding: explorer.EncodingText},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := cellFromValue(test.value, test.dbType)
			if err != nil || got.Text != test.want || got.Null != test.null || got.Encoding != test.encoding {
				t.Fatalf("cellFromValue() = (%+v, %v)", got, err)
			}
		})
	}
}
