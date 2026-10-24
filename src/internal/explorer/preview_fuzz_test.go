package explorer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzPreviewText(f *testing.F) {
	f.Add("")
	f.Add("<script>text</script>")
	f.Add(strings.Repeat("界", 2000))
	f.Add(string([]byte{0xff}))
	f.Fuzz(func(t *testing.T, input string) {
		cell, err := PreviewText(input)
		if err != nil {
			if utf8.ValidString(input) {
				t.Fatal("valid text rejected")
			}
			return
		}
		if len(cell.Text) > MaxCellBytes || !utf8.ValidString(cell.Text) || !strings.HasPrefix(input, cell.Text) || cell.Null || cell.Truncated != (len(input) > MaxCellBytes) {
			t.Fatal("unsafe preview")
		}
	})
}

func FuzzQuoteIdentifier(f *testing.F) {
	f.Add("items")
	f.Add(`x"; DROP TABLE users;--`)
	f.Add("名前")
	f.Add("bad\x00name")
	f.Fuzz(func(t *testing.T, input string) {
		quoted, err := QuoteIdentifier(input)
		if err != nil {
			return
		}
		if len(quoted) < 2 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' || strings.ReplaceAll(quoted[1:len(quoted)-1], `""`, `"`) != input {
			t.Fatal("identifier escaped its quoted boundary")
		}
	})
}
