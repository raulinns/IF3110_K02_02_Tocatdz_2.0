package migration

import (
	"crypto/sha256"
	"errors"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var ErrExecutionUncertain = errors.Join(security.ErrUnavailable, errors.New("migration outcome requires reconciliation"))

func missing(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}
func ValidateDraft(draft Draft) error {
	if strings.TrimSpace(draft.Title) == "" || !utf8.ValidString(draft.Title) || utf8.RuneCountInString(draft.Title) > MaxTitleRunes || strings.ContainsRune(draft.Title, 0) {
		return security.ErrInvalidInput
	}
	sql := draft.SQL.Reveal()
	if strings.TrimSpace(sql) == "" || !utf8.ValidString(sql) || len(sql) > MaxSQLBytes || strings.ContainsRune(sql, 0) {
		return security.ErrInvalidInput
	}
	return nil
}
func Checksum(sql security.Secret) [32]byte { return sha256.Sum256([]byte(sql.Reveal())) }
func pagination(query HistoryQuery) (HistoryQuery, int64, error) {
	if query.Page == 0 {
		query.Page = 1
	}
	if query.Limit == 0 {
		query.Limit = DefaultLimit
	}
	if query.Page < 1 || query.Limit < 1 || query.Limit > MaxLimit || int64(query.Page-1) > MaxOffset/int64(query.Limit) {
		return HistoryQuery{}, 0, security.ErrInvalidInput
	}
	return query, int64(query.Page-1) * int64(query.Limit), nil
}
func ParseHistoryQuery(values url.Values) (HistoryQuery, error) {
	query := HistoryQuery{}
	for name, items := range values {
		if (name != "page" && name != "limit") || len(items) != 1 {
			return query, security.ErrInvalidInput
		}
		value, err := strconv.Atoi(items[0])
		if err != nil || value < 1 || strconv.Itoa(value) != items[0] {
			return query, security.ErrInvalidInput
		}
		if name == "page" {
			query.Page = value
		} else {
			query.Limit = value
		}
	}
	query, _, err := pagination(query)
	return query, err
}
func safeError(err error) error {
	if err == nil {
		return nil
	}
	for _, allowed := range []error{security.ErrUnauthenticated, security.ErrForbidden, security.ErrNotFound, security.ErrConflict, security.ErrInvalidInput, security.ErrResultTooLarge} {
		if errors.Is(err, allowed) {
			return allowed
		}
	}
	return security.ErrUnavailable
}
func keyFor(record Migration) ExecutionKey {
	return ExecutionKey{DatabaseID: record.DatabaseID, MigrationID: record.ID, ExecutionID: record.ExecutionID, Version: record.Version, Checksum: record.Checksum}
}
func resultFor(record Migration) ExecutionResult {
	return ExecutionResult{MigrationID: record.ID, ExecutionID: record.ExecutionID, Version: record.Version, Status: record.Status, Failure: record.Failure}
}
