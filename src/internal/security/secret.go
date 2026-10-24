package security

import (
	"fmt"
	"log/slog"
)

// Secret menyamarkan nilai saat dicetak atau dicatat di log, tapi tidak mengenkripsi isi memori
type Secret struct {
	value *string
}

// Pointer ini mencegah fmt membongkar isi secret lewat field private pada struct lain.
func NewSecret(value string) Secret {
	if value == "" {
		return Secret{}
	}
	return Secret{value: &value}
}

func (s Secret) Reveal() string {
	if s.value == nil {
		return ""
	}
	return *s.value
}

func (s Secret) Empty() bool { return s.value == nil || *s.value == "" }

func (Secret) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[REDACTED]")) }

func (Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

func (Secret) MarshalJSON() ([]byte, error) { return nil, ErrSecretSerialization }

func (Secret) MarshalText() ([]byte, error) { return nil, ErrSecretSerialization }
