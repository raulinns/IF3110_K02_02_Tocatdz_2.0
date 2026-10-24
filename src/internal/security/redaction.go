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
