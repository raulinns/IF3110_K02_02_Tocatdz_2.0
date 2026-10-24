package explorer

import (
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func PreviewText(value string) (Cell, error) {
	if !utf8.ValidString(value) {
		return Cell{}, security.ErrUnavailable
	}
	cell := Cell{Text: value, Encoding: EncodingText}
	if len(value) > MaxCellBytes {
		end := MaxCellBytes
		for end > 0 && !utf8.RuneStart(value[end]) {
			end--
		}
		cell.Text = value[:end]
		cell.Truncated = true
	}
	return cell, nil
}

func PreviewBinary(value []byte) Cell {
	limit := (MaxCellBytes - 2) / 2
	truncated := len(value) > limit
	if truncated {
		value = value[:limit]
	}
	return Cell{Text: `\x` + hex.EncodeToString(value), Encoding: EncodingHex, Truncated: truncated}
}

func normalizeCell(input Cell) (Cell, error) {
	if input.Null {
		return Cell{Null: true}, nil
	}
	if input.Encoding == "" || input.Encoding == EncodingText {
		cell, err := PreviewText(input.Text)
		cell.Truncated = cell.Truncated || input.Truncated
		return cell, err
	}
	if input.Encoding != EncodingHex || len(input.Text) < 2 || input.Text[:2] != `\x` || (len(input.Text)-2)%2 != 0 {
		return Cell{}, security.ErrUnavailable
	}
	for _, char := range input.Text[2:] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return Cell{}, security.ErrUnavailable
		}
	}
	cell := input
	if len(cell.Text) > MaxCellBytes {
		cell.Text = cell.Text[:MaxCellBytes]
		cell.Truncated = true
	}
	return cell, nil
}

type payloadBudget struct{ used int }

func (b *payloadBudget) add(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return security.ErrUnavailable
	}
	if len(encoded) > MaxPayloadBytes-b.used {
		return security.ErrResultTooLarge
	}
	b.used += len(encoded)
	return nil
}

func payloadSize(value any) error {
	return (&payloadBudget{}).add(value)
}
