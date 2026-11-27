package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// ChecksumDefinition implements the version-1 definition checksum, independent of the wire format.
// Call ValidateDefinition before persisting new definitions.
func ChecksumDefinition(input DefinitionInput) DefinitionChecksum {
	type canonicalBatch struct {
		Schema     string  `json:"schema"`
		Table      string  `json:"table"`
		KeyColumn  string  `json:"key_column"`
		BatchSize  int     `json:"batch_size"`
		ScopeWhere *string `json:"scope_where"`
	}
	var batch *canonicalBatch
	if b := input.Batch; b != nil {
		batch = &canonicalBatch{b.Schema, b.Table, b.KeyColumn, b.BatchSize, optionalString(b.ScopeWhere)}
	}
	payload := struct {
		Version      int             `json:"checksum_version"`
		Title        string          `json:"title"`
		UpSQL        string          `json:"up_sql"`
		DownSQL      *string         `json:"down_sql"`
		Irreversible bool            `json:"irreversible"`
		Reason       string          `json:"irreversible_reason"`
		Mode         ExecutionMode   `json:"mode"`
		Batch        *canonicalBatch `json:"batch"`
	}{1, input.Title, input.UpSQL.Reveal(), optionalString(input.DownSQL), input.Irreversible, input.IrreversibleReason, input.Mode, batch}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	// These concrete fields are JSON-encodable and bytes.Buffer cannot fail.
	_ = encoder.Encode(payload)
	return sha256.Sum256(bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}))
}

func optionalString(secret security.Secret) *string {
	if secret.Empty() {
		return nil
	}
	value := secret.Reveal()
	return &value
}

// ValidateDefinition checks both SQL directions without accessing a target.
// Catalog checks for an eligible integer batch key belong to the locked runner.
func ValidateDefinition(ctx context.Context, input DefinitionInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateDefinitionShape(input); err != nil {
		return err
	}
	validator := NewPostgreSQLValidator()
	directions := []struct {
		field string
		sql   security.Secret
	}{{"up_sql", input.UpSQL}, {"down_sql", input.DownSQL}}
	for _, direction := range directions {
		if direction.sql.Empty() {
			continue
		}
		var err error
		if input.Mode == ModeBatch {
			err = validateBatchAssignments(ctx, validator, *input.Batch, direction.sql)
		} else {
			err = validator.Validate(ctx, direction.sql)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", direction.field, safeValidationError(err))
		}
	}
	return nil
}

func validateDefinitionShape(input DefinitionInput) error {
	if err := ValidateDraft(Draft{Title: input.Title, SQL: input.UpSQL}); err != nil {
		return err
	}
	if input.Irreversible {
		reason := input.IrreversibleReason
		if !input.DownSQL.Empty() || strings.TrimSpace(reason) == "" || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > MaxReasonRunes || strings.ContainsRune(reason, 0) {
			return security.ErrInvalidInput
		}
	} else if input.IrreversibleReason != "" || ValidateDraft(Draft{Title: input.Title, SQL: input.DownSQL}) != nil {
		return security.ErrInvalidInput
	}
	switch input.Mode {
	case ModeNormal:
		if input.Batch != nil {
			return security.ErrInvalidInput
		}
	case ModeBatch:
		b := input.Batch
		if b == nil || b.BatchSize < MinBatchSize || b.BatchSize > MaxBatchSize ||
			!validBatchIdentifier(b.Schema) || !validBatchIdentifier(b.Table) || !validBatchIdentifier(b.KeyColumn) {
			return security.ErrInvalidInput
		}
		// The current batch runner cannot enforce a predicate. Fail closed until
		// the integrator adds predicate support and expands its validation.
		if !b.ScopeWhere.Empty() {
			return fmt.Errorf("batch.scope_where is not supported by the runner: %w", security.ErrInvalidInput)
		}
	default:
		return security.ErrInvalidInput
	}
	return nil
}

func validBatchIdentifier(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 63 && utf8.ValidString(name) && !strings.ContainsRune(name, 0)
}

func validateBatchAssignments(ctx context.Context, validator *PostgreSQLValidator, batch BatchConfig, sql security.Secret) error {
	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
	prefix := "UPDATE " + quote(batch.Schema) + "." + quote(batch.Table) + " SET "
	const filter = " WHERE TRUE"
	// The runner appends its WHERE in the same line. Requiring the sentinel
	// in the parsed AST detects a trailing comment or injected statement/clause.
	statements, err := validator.parse(ctx, security.NewSecret(prefix+sql.Reveal()+filter))
	if err != nil {
		return err
	}
	if len(statements) != 1 {
		return security.ErrInvalidInput
	}
	update, _ := statements[0].Stmt["UpdateStmt"].(map[string]any)
	where, _ := update["whereClause"].(map[string]any)
	constant, _ := where["A_Const"].(map[string]any)
	delete(constant, "location")
	if len(update) != 3 || update["relation"] == nil || len(where) != 1 ||
		!reflect.DeepEqual(constant, map[string]any{"boolval": map[string]any{"boolval": true}}) {
		return security.ErrInvalidInput
	}
	targets, _ := update["targetList"].([]any)
	if len(targets) == 0 {
		return security.ErrInvalidInput
	}
	for _, entry := range targets {
		node, _ := entry.(map[string]any)
		target, _ := node["ResTarget"].(map[string]any)
		name, _ := target["name"].(string)
		if name == "" || name == batch.KeyColumn || target["indirection"] != nil || !scalarBatchExpression(target["val"]) {
			return security.ErrInvalidInput
		}
	}
	return nil
}

// Batch assignments are scalar expressions; subqueries, multi-column SET and
// bind parameters are excluded. The M1 AST validator checks function policy.
func scalarBatchExpression(value any) bool {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			switch key {
			case "SelectStmt", "SubLink", "MultiAssignRef", "ParamRef":
				return false
			}
			if !scalarBatchExpression(child) {
				return false
			}
		}
	case []any:
		for _, child := range node {
			if !scalarBatchExpression(child) {
				return false
			}
		}
	}
	return true
}
