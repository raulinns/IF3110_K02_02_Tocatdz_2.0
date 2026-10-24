package security

import (
	"fmt"
	"log/slog"
)

func (requestView) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[REDACTED]")) }
func (requestView) LogValue() slog.Value           { return slog.StringValue("[REDACTED]") }

func (AuthService) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[REDACTED]")) }
func (AuthService) LogValue() slog.Value           { return slog.StringValue("[REDACTED]") }

func (HMACCSRFGuard) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[REDACTED]")) }
func (HMACCSRFGuard) LogValue() slog.Value           { return slog.StringValue("[REDACTED]") }

// ID dan digest session ikut disamarkan agar dump struct tidak membocorkan data session.
func (Principal) Format(state fmt.State, _ rune)       { _, _ = state.Write([]byte("[REDACTED]")) }
func (Principal) LogValue() slog.Value                 { return slog.StringValue("[REDACTED]") }
func (TokenDigest) Format(state fmt.State, _ rune)     { _, _ = state.Write([]byte("[REDACTED]")) }
func (TokenDigest) LogValue() slog.Value               { return slog.StringValue("[REDACTED]") }
func (Session) Format(state fmt.State, _ rune)         { _, _ = state.Write([]byte("[REDACTED]")) }
func (Session) LogValue() slog.Value                   { return slog.StringValue("[REDACTED]") }
func (SessionSnapshot) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[REDACTED]")) }
func (SessionSnapshot) LogValue() slog.Value           { return slog.StringValue("[REDACTED]") }
func (SessionResult) Format(state fmt.State, _ rune)   { _, _ = state.Write([]byte("[REDACTED]")) }
func (SessionResult) LogValue() slog.Value             { return slog.StringValue("[REDACTED]") }
func (Credentials) Format(state fmt.State, _ rune)     { _, _ = state.Write([]byte("[REDACTED]")) }
func (Credentials) LogValue() slog.Value               { return slog.StringValue("[REDACTED]") }
