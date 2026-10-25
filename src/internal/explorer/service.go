package explorer

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const ReadTimeout = 5 * time.Second

type Config struct {
	Directory  Directory
	Reader     Reader
	Authorizer Authorizer
}

type Service struct{ deps Config }

var _ Viewer = (*Service)(nil)

func (s *Service) Capabilities(ctx context.Context, id security.ID) (dbms.Capabilities, error) {
	resolver, ok := s.deps.Reader.(dbms.CapabilityResolver)
	if !ok {
		return dbms.Features(dbms.PostgreSQL), nil
	}
	capabilities, err := resolver.Capabilities(ctx, id)
	if err != nil {
		return dbms.Capabilities{}, readError(err)
	}
	return capabilities, nil
}

func NewService(cfg Config) (*Service, error) {
	for _, dependency := range []any{cfg.Directory, cfg.Reader, cfg.Authorizer} {
		if dependency == nil {
			return nil, security.ErrInvalidInput
		}
		value := reflect.ValueOf(dependency)
		switch value.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
			if value.IsNil() {
				return nil, security.ErrInvalidInput
			}
		}
	}
	return &Service{deps: cfg}, nil
}

func readError(err error) error {
	for _, category := range []error{context.Canceled, context.DeadlineExceeded, security.ErrUnauthenticated, security.ErrForbidden, security.ErrNotFound, security.ErrInvalidInput, security.ErrResultTooLarge} {
		if errors.Is(err, category) {
			return category
		}
	}
	return security.ErrUnavailable
}

func validPrincipal(principal security.Principal) bool {
	return security.ValidID(principal.UserID) && security.ValidID(principal.SessionID) && principal.AuthVersion > 0 && principal.CSRFEpoch > 0
}

func (s *Service) authorize(ctx context.Context, request Request, action security.DatabaseAction) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validPrincipal(request.Principal) {
		return security.ErrUnauthenticated
	}
	if !security.ValidID(request.DatabaseID) {
		return security.ErrForbidden
	}
	if err := s.deps.Authorizer.RequireDatabase(ctx, request.Principal, request.DatabaseID, action); err != nil {
		return readError(err)
	}
	return nil
}

func (s *Service) ListAccessible(ctx context.Context, principal security.Principal) ([]DatabaseSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validPrincipal(principal) {
		return nil, security.ErrUnauthenticated
	}
	if err := s.deps.Authorizer.RequireAuthenticated(ctx, principal); err != nil {
		return nil, readError(err)
	}
	entries, err := s.deps.Directory.ListAccessible(ctx, principal)
	if err != nil {
		return nil, readError(err)
	}
	if len(entries) > MaxCatalogItems {
		return nil, security.ErrResultTooLarge
	}
	result := make([]DatabaseSummary, 0, len(entries))
	seen := make(map[security.ID]bool)
	budget := payloadBudget{used: 256}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Access == security.AccessNone {
			continue
		}
		if entry.Access != security.AccessViewOnly && entry.Access != security.AccessViewEdit {
			return nil, security.ErrUnavailable
		}
		if !security.ValidID(entry.ID) || seen[entry.ID] || entry.Name == "" || len(entry.Name) > 512 || !utf8.ValidString(entry.Name) {
			return nil, security.ErrUnavailable
		}
		seen[entry.ID] = true
		err := s.authorize(ctx, Request{Principal: principal, DatabaseID: entry.ID}, security.ActionViewSchema)
		if errors.Is(err, security.ErrForbidden) || errors.Is(err, security.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := budget.add(entry); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	if err := s.deps.Authorizer.RequireAuthenticated(ctx, principal); err != nil {
		return nil, readError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	if err := payloadSize(result); err != nil {
		return nil, err
	}
	return result, nil
}

func knownKind(kind TableKind) bool {
	return kind == KindTable || kind == KindPartitionedTable || kind == KindView || kind == KindMaterializedView
}

func (s *Service) Schemas(ctx context.Context, request Request) ([]Schema, error) {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	if err := s.authorize(ctx, request, security.ActionViewSchema); err != nil {
		return nil, err
	}
	schemas, err := s.deps.Reader.Schemas(ctx, request.DatabaseID)
	if err != nil {
		return nil, readError(err)
	}
	if len(schemas) > MaxCatalogItems {
		return nil, security.ErrResultTooLarge
	}
	result := make([]Schema, 0, len(schemas))
	seen := make(map[string]bool)
	count := 0
	budget := payloadBudget{used: 256}
	for _, schema := range schemas {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !validIdentifier(schema.Name) {
			return nil, security.ErrUnavailable
		}
		if !IsVisibleSchema(schema.Name) {
			continue
		}
		if seen[schema.Name] {
			return nil, security.ErrUnavailable
		}
		seen[schema.Name] = true
		count += len(schema.Tables)
		if count > MaxCatalogItems {
			return nil, security.ErrResultTooLarge
		}
		output := Schema{Name: schema.Name, Tables: make([]TableSummary, 0, len(schema.Tables))}
		tables := make(map[string]bool)
		for _, table := range schema.Tables {
			if !validIdentifier(table.Name) || !knownKind(table.Kind) || tables[table.Name] {
				return nil, security.ErrUnavailable
			}
			tables[table.Name] = true
			output.Tables = append(output.Tables, table)
		}
		sort.Slice(output.Tables, func(i, j int) bool { return output.Tables[i].Name < output.Tables[j].Name })
		if err := budget.add(output); err != nil {
			return nil, err
		}
		result = append(result, output)
	}
	if err := s.authorize(ctx, request, security.ActionViewSchema); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	if err := payloadSize(result); err != nil {
		return nil, err
	}
	return result, nil
}

func relationInput(schema, table string) error {
	if !validIdentifier(schema) || !validIdentifier(table) {
		return security.ErrInvalidInput
	}
	if !IsVisibleSchema(schema) {
		return security.ErrNotFound
	}
	return nil
}

func catalogText(value string) error {
	if len(value) > MaxPayloadBytes {
		return security.ErrResultTooLarge
	}
	if !utf8.ValidString(value) {
		return security.ErrUnavailable
	}
	return nil
}

func (s *Service) Table(ctx context.Context, request Request, schema, name string) (Table, error) {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	if err := s.authorize(ctx, request, security.ActionViewSchema); err != nil {
		return Table{}, err
	}
	if err := relationInput(schema, name); err != nil {
		return Table{}, err
	}
	table, err := s.deps.Reader.Table(ctx, request.DatabaseID, schema, name)
	if err != nil {
		return Table{}, readError(err)
	}
	if table.Schema != schema || table.Name != name || !knownKind(table.Kind) {
		return Table{}, security.ErrUnavailable
	}
	if len(table.Columns) > MaxColumns || len(table.Constraints) > MaxCatalogItems || len(table.Indexes) > MaxCatalogItems {
		return Table{}, security.ErrResultTooLarge
	}
	result := Table{Schema: schema, Name: name, Kind: table.Kind, Columns: make([]Column, 0, len(table.Columns)), Constraints: make([]Constraint, 0, len(table.Constraints)), Indexes: make([]Index, 0, len(table.Indexes))}
	budget := payloadBudget{used: 256}
	seen := make(map[string]bool)
	for _, column := range table.Columns {
		if err := ctx.Err(); err != nil {
			return Table{}, err
		}
		if !validIdentifier(column.Name) || column.DataType == "" || seen[column.Name] {
			return Table{}, security.ErrUnavailable
		}
		seen[column.Name] = true
		if err := catalogText(column.DataType); err != nil {
			return Table{}, err
		}
		if column.Default != nil {
			if err := catalogText(*column.Default); err != nil {
				return Table{}, err
			}
			value := *column.Default
			column.Default = &value
		}
		if err := budget.add(column); err != nil {
			return Table{}, err
		}
		result.Columns = append(result.Columns, column)
	}
	seen = make(map[string]bool)
	for _, constraint := range table.Constraints {
		if err := ctx.Err(); err != nil {
			return Table{}, err
		}
		if !validIdentifier(constraint.Name) || constraint.Type == "" || seen[constraint.Name] {
			return Table{}, security.ErrUnavailable
		}
		seen[constraint.Name] = true
		if err := catalogText(constraint.Type); err != nil {
			return Table{}, err
		}
		if err := catalogText(constraint.Definition); err != nil {
			return Table{}, err
		}
		if err := budget.add(constraint); err != nil {
			return Table{}, err
		}
		result.Constraints = append(result.Constraints, constraint)
	}
	seen = make(map[string]bool)
	for _, index := range table.Indexes {
		if err := ctx.Err(); err != nil {
			return Table{}, err
		}
		if !validIdentifier(index.Name) || seen[index.Name] {
			return Table{}, security.ErrUnavailable
		}
		seen[index.Name] = true
		if err := catalogText(index.Definition); err != nil {
			return Table{}, err
		}
		if err := budget.add(index); err != nil {
			return Table{}, err
		}
		result.Indexes = append(result.Indexes, index)
	}
	if err := s.authorize(ctx, request, security.ActionViewSchema); err != nil {
		return Table{}, err
	}
	sort.Slice(result.Constraints, func(i, j int) bool { return result.Constraints[i].Name < result.Constraints[j].Name })
	sort.Slice(result.Indexes, func(i, j int) bool { return result.Indexes[i].Name < result.Indexes[j].Name })
	if err := payloadSize(result); err != nil {
		return Table{}, err
	}
	return result, nil
}

func (s *Service) Rows(ctx context.Context, request Request, query RowsQuery) (RowsPage, error) {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	if err := s.authorize(ctx, request, security.ActionViewData); err != nil {
		return RowsPage{}, err
	}
	if err := relationInput(query.Schema, query.Table); err != nil {
		return RowsPage{}, err
	}
	page, limit, offset, err := normalizePagination(query.Page, query.Limit)
	if err != nil {
		return RowsPage{}, err
	}
	options := RowReadOptions{Offset: offset, FetchLimit: limit + 1, MaxCellBytes: MaxCellBytes, MaxPayloadBytes: MaxPayloadBytes}
	batch, err := s.deps.Reader.Rows(ctx, request.DatabaseID, query.Schema, query.Table, options)
	if err != nil {
		return RowsPage{}, readError(err)
	}
	if len(batch.Rows) > options.FetchLimit {
		return RowsPage{}, security.ErrUnavailable
	}
	if len(batch.Columns) > MaxColumns {
		return RowsPage{}, security.ErrResultTooLarge
	}
	if batch.Order == "" {
		batch.Order = OrderBestEffort
	}
	if batch.Order != OrderPrimaryKey && batch.Order != OrderBestEffort {
		return RowsPage{}, security.ErrUnavailable
	}
	result := RowsPage{Columns: append([]string{}, batch.Columns...), Rows: make([][]Cell, 0, min(len(batch.Rows), limit)), Page: page, Limit: limit, HasNext: len(batch.Rows) > limit, Order: batch.Order}
	seen := make(map[string]bool)
	for _, column := range result.Columns {
		if !validIdentifier(column) || seen[column] {
			return RowsPage{}, security.ErrUnavailable
		}
		seen[column] = true
	}
	budget := payloadBudget{used: 256}
	if err := budget.add(result.Columns); err != nil {
		return RowsPage{}, err
	}
	for i, row := range batch.Rows {
		if err := ctx.Err(); err != nil {
			return RowsPage{}, err
		}
		if len(row) != len(result.Columns) {
			return RowsPage{}, security.ErrUnavailable
		}
		if i >= limit {
			continue
		}
		output := make([]Cell, len(row))
		for j, cell := range row {
			output[j], err = normalizeCell(cell)
			if err != nil {
				return RowsPage{}, err
			}
			if err := budget.add(output[j]); err != nil {
				return RowsPage{}, err
			}
		}
		result.Rows = append(result.Rows, output)
	}
	// Jangan kirim hasil query kalau akses dicabut ketika data sedang dibaca.
	if err := s.authorize(ctx, request, security.ActionViewData); err != nil {
		return RowsPage{}, err
	}
	if err := payloadSize(result); err != nil {
		return RowsPage{}, err
	}
	return result, nil
}
