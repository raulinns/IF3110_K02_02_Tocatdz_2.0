package migration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	pgquery "github.com/wasilibs/go-pgquery/parser"
)

type SQLRejection string

func (SQLRejection) Error() string { return "SQL migration ditolak" }
func (SQLRejection) Unwrap() error { return security.ErrInvalidInput }
func (reason SQLRejection) Message() string {
	switch reason {
	case "syntax":
		return "SQL tidak valid. Periksa sintaks PostgreSQL."
	case "transaction":
		return "Perintah transaksi diatur oleh aplikasi."
	case "concurrent":
		return "Perintah ini tidak dapat dijalankan dalam transaksi migration."
	case "protected":
		return "SQL mengakses objek internal atau schema sistem."
	case "function":
		return "Fungsi SQL harus termasuk daftar yang diizinkan dan memakai schema pg_catalog."
	default:
		return "Jenis perintah SQL ini belum diizinkan untuk migration."
	}
}
func safeValidationError(err error) error {
	if err == nil {
		return nil
	}
	var reason SQLRejection
	if errors.As(err, &reason) {
		switch reason {
		case "syntax", "transaction", "concurrent", "protected", "function", "unsupported":
			return reason
		}
	}
	return safeError(err)
}

type PostgreSQLValidator struct{ gate chan struct{} }

var defaultParserGate = make(chan struct{}, 1)
var parserOnce sync.Once

func NewPostgreSQLValidator() *PostgreSQLValidator {
	parserOnce.Do(func() { defaultParserGate <- struct{}{} })
	return &PostgreSQLValidator{gate: defaultParserGate}
}

func (v *PostgreSQLValidator) Validate(ctx context.Context, sql security.Secret) error {
	if v == nil || v.gate == nil {
		return security.ErrUnavailable
	}
	if ValidateDraft(Draft{Title: "SQL", SQL: sql}) != nil {
		return security.ErrInvalidInput
	}
	select {
	case <-ctx.Done():
		return security.ErrUnavailable
	case <-v.gate:
	}
	defer func() { v.gate <- struct{}{} }()
	if ctx.Err() != nil {
		return security.ErrUnavailable
	}
	tree, err := pgquery.ParseToJSON(sql.Reveal())
	if ctx.Err() != nil {
		return security.ErrUnavailable
	}
	if err != nil {
		return SQLRejection("syntax")
	}
	var parsed struct {
		Stmts []struct {
			Stmt map[string]any `json:"stmt"`
		} `json:"stmts"`
	}
	if len(tree) > 32<<20 || json.Unmarshal([]byte(tree), &parsed) != nil {
		return security.ErrInvalidInput
	}
	if len(parsed.Stmts) == 0 || len(parsed.Stmts) > 1000 {
		return SQLRejection("unsupported")
	}
	for _, stmt := range parsed.Stmts {
		if len(stmt.Stmt) != 1 {
			return SQLRejection("unsupported")
		}
		if err := checkAST(ctx, stmt.Stmt, 0); err != nil {
			return err
		}
	}
	return nil
}

var allowedStatements = map[string]bool{
	"CreateStmt": true, "AlterTableStmt": true, "IndexStmt": true, "DropStmt": true,
	"RenameStmt": true, "ViewStmt": true, "CreateSchemaStmt": true,
	"InsertStmt": true, "UpdateStmt": true, "DeleteStmt": true, "TruncateStmt": true,
	"SelectStmt": true, "CommentStmt": true,
}
var allowedAlter = map[string]bool{
	"AT_AddColumn": true, "AT_DropColumn": true, "AT_AlterColumnType": true,
	"AT_SetNotNull": true, "AT_DropNotNull": true, "AT_ColumnDefault": true,
	"AT_AddConstraint": true, "AT_DropConstraint": true, "AT_ValidateConstraint": true,
}
var allowedObjects = map[string]bool{"OBJECT_TABLE": true, "OBJECT_COLUMN": true, "OBJECT_INDEX": true, "OBJECT_TABCONSTRAINT": true, "OBJECT_VIEW": true, "OBJECT_SCHEMA": true}
var allowedFunctions = map[string]bool{
	"now": true, "transaction_timestamp": true, "statement_timestamp": true,
	"gen_random_uuid": true, "lower": true, "upper": true, "length": true,
	"char_length": true, "abs": true, "round": true, "concat": true,
	"concat_ws": true, "replace": true, "btrim": true, "ltrim": true,
	"rtrim": true, "substring": true, "date_trunc": true, "count": true,
	"sum": true, "min": true, "max": true, "avg": true,
}

func protected(name string) bool {
	return name == "tocatdz_internal" || name == "information_schema" || strings.HasPrefix(name, "pg_")
}
func stringNode(value any) string {
	obj, _ := value.(map[string]any)
	node, _ := obj["String"].(map[string]any)
	text, _ := node["sval"].(string)
	return text
}
func checkAST(ctx context.Context, value any, depth int) error {
	if ctx.Err() != nil {
		return security.ErrUnavailable
	}
	if depth > 128 {
		return SQLRejection("unsupported")
	}
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if err := checkAST(ctx, child, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		for kind, child := range node {
			if kind == "schemaname" || kind == "subname" {
				if protectedString(child) {
					return SQLRejection("protected")
				}
			}
			if kind == "TransactionStmt" {
				return SQLRejection("transaction")
			}
			if len(kind) > 0 && kind[0] >= 'A' && kind[0] <= 'Z' && strings.HasSuffix(kind, "Stmt") && !allowedStatements[kind] {
				return SQLRejection("unsupported")
			}
			obj, _ := child.(map[string]any)
			if concurrent, _ := obj["concurrent"].(bool); concurrent {
				return SQLRejection("concurrent")
			}
			if kind == "RangeVar" || obj["relname"] != nil {
				if catalog, _ := obj["catalogname"].(string); catalog != "" {
					return SQLRejection("protected")
				}
				if schema, _ := obj["schemaname"].(string); protected(schema) {
					return SQLRejection("protected")
				}
				if persistence, _ := obj["relpersistence"].(string); persistence == "t" {
					return SQLRejection("unsupported")
				}
			}
			if kind == "SelectStmt" && (obj["intoClause"] != nil || obj["lockingClause"] != nil) {
				return SQLRejection("unsupported")
			}
			if kind == "CreateSchemaStmt" {
				name, _ := obj["schemaname"].(string)
				if protected(name) {
					return SQLRejection("protected")
				}
				if obj["authrole"] != nil {
					return SQLRejection("unsupported")
				}
			}
			if kind == "AlterTableCmd" {
				subtype, _ := obj["subtype"].(string)
				if !allowedAlter[subtype] {
					return SQLRejection("unsupported")
				}
			}
			if kind == "DropStmt" || kind == "RenameStmt" || kind == "CommentStmt" {
				field := "removeType"
				if kind == "RenameStmt" {
					field = "renameType"
				}
				if kind == "CommentStmt" {
					field = "objtype"
				}
				target, _ := obj[field].(string)
				if !allowedObjects[target] {
					return SQLRejection("unsupported")
				}
				if reservedObject(obj["objects"]) || reservedObject(obj["object"]) || protectedString(obj["newname"]) {
					return SQLRejection("protected")
				}
			}
			if kind == "FuncCall" {
				names, _ := obj["funcname"].([]any)
				if len(names) != 2 || stringNode(names[0]) != "pg_catalog" || !allowedFunctions[stringNode(names[1])] {
					return SQLRejection("function")
				}
			}
			if err := checkAST(ctx, child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
func protectedString(value any) bool { text, _ := value.(string); return protected(text) }
func reservedObject(value any) bool {
	if protected(stringNode(value)) {
		return true
	}
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if reservedObject(child) {
				return true
			}
		}
	case map[string]any:
		for _, child := range node {
			if reservedObject(child) {
				return true
			}
		}
	}
	return false
}
