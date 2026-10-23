package security

import (
	"fmt"
	"log/slog"
)

// Secret reduces accidental disclosure through fmt, slog, and serialization.
// Reveal is reserved for hashing, cryptography, DB adapters, and cookie transport.
// It is not memory encryption; never log Reveal() or serialize domain records.
type Secret struct {
	value string
}

func NewSecret(value string) Secret { return Secret{value: value} }

func (s Secret) Reveal() string { return s.value }

func (s Secret) Empty() bool { return s.value == "" }

func (Secret) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[REDACTED]")) }

func (Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

func (Secret) MarshalJSON() ([]byte, error) { return nil, ErrSecretSerialization }

func (Secret) MarshalText() ([]byte, error) { return nil, ErrSecretSerialization }
