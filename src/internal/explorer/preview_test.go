package explorer

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestTextPreviewBoundsUTF8WithoutTreatingEmptyAsNull(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("x", MaxCellBytes), strings.Repeat("界", MaxCellBytes), strings.Repeat("x", MaxCellBytes-1) + "界"} {
		cell, err := PreviewText(value)
		if err != nil || cell.Null || len(cell.Text) > MaxCellBytes || !utf8.ValidString(cell.Text) || cell.Truncated != (len(value) > MaxCellBytes) {
			t.Fatal("invalid text preview")
		}
		if !strings.HasPrefix(value, cell.Text) {
			t.Fatal("preview changed original value")
		}
	}
	if _, err := PreviewText(string([]byte{0xff})); !errors.Is(err, security.ErrUnavailable) {
		t.Fatal("invalid text UTF-8 accepted")
	}
}

func TestBinaryPreviewIsBoundedHexWithExplicitEncoding(t *testing.T) {
	cell := PreviewBinary([]byte{0, 0xff, 0x3c})
	if cell.Text != `\x00ff3c` || cell.Encoding != EncodingHex || cell.Truncated {
		t.Fatal("binary encoding incorrect")
	}
	cell = PreviewBinary(bytes.Repeat([]byte{0xff}, MaxCellBytes))
	if len(cell.Text) != MaxCellBytes || !cell.Truncated {
		t.Fatal("binary preview unbounded")
	}
	normalized, err := normalizeCell(Cell{Null: true, Text: "must not leak", Truncated: true, Encoding: EncodingHex})
	if err != nil || normalized.Text != "" || !normalized.Null || normalized.Truncated {
		t.Fatal("NULL preview carried hidden contents")
	}
	cell, err = normalizeCell(Cell{Text: "already clipped", Truncated: true})
	if err != nil || !cell.Truncated {
		t.Fatal("adapter truncation flag was lost")
	}
}

func TestCellJSONEscapesHTMLWithoutExecutingContent(t *testing.T) {
	cell, _ := PreviewText(`<script>alert("x")</script>`)
	encoded, err := json.Marshal(cell)
	if err != nil || strings.Contains(string(encoded), "<script>") {
		t.Fatal("JSON embedded raw executable HTML")
	}
}
