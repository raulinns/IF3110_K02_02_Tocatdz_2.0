package ui

import (
	"fmt"
	"log/slog"
)

// SQL tetap tersedia untuk template, tapi tidak ikut dump struct atau atribut log.
func (MigrationDraft) Format(state fmt.State, _ rune)  { _, _ = state.Write([]byte("[REDACTED]")) }
func (MigrationDraft) LogValue() slog.Value            { return slog.StringValue("[REDACTED]") }
func (MigrationForm) Format(state fmt.State, _ rune)   { _, _ = state.Write([]byte("[REDACTED]")) }
func (MigrationForm) LogValue() slog.Value             { return slog.StringValue("[REDACTED]") }
func (MigrationDetail) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[REDACTED]")) }
func (MigrationDetail) LogValue() slog.Value           { return slog.StringValue("[REDACTED]") }
func (Page) Format(state fmt.State, _ rune)            { _, _ = state.Write([]byte("[REDACTED]")) }
func (Page) LogValue() slog.Value                      { return slog.StringValue("[REDACTED]") }
