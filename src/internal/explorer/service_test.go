package explorer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	userID     security.ID = "b0c4f0c0-9167-43d6-9089-afd7b9826f0d"
	sessionID  security.ID = "5ed48df2-bcdd-4b52-b9ad-8d9f6689df55"
	databaseID security.ID = "4edd2fdc-1aab-46fc-b9bf-ff1f16a8a1e6"
	deniedID   security.ID = "4edd2fdc-1aab-46fc-b9bf-ff1f16a8a1e7"
)

func principal() security.Principal {
	return security.Principal{UserID: userID, SessionID: sessionID, AuthVersion: 1, CSRFEpoch: 1}
}
func request() Request { return Request{Principal: principal(), DatabaseID: databaseID} }

type authorizerStub struct {
	mu      sync.Mutex
	err     error
	denied  map[security.ID]error
	actions []security.DatabaseAction
}

func (a *authorizerStub) RequireAuthenticated(ctx context.Context, _ security.Principal) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.err
}

func (a *authorizerStub) RequireDatabase(ctx context.Context, _ security.Principal, id security.ID, action security.DatabaseAction) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	a.actions = append(a.actions, action)
	if a.err != nil {
		return a.err
	}
	return a.denied[id]
}

type directoryStub struct {
	entries []DatabaseSummary
	err     error
	mu      sync.Mutex
	calls   int
}

func (d *directoryStub) ListAccessible(ctx context.Context, _ security.Principal) ([]DatabaseSummary, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.entries, d.err
}

type readerStub struct {
	mu           sync.Mutex
	calls        int
	schemas      []Schema
	table        Table
	batch        RowBatch
	err          error
	onRead       func(context.Context)
	options      RowReadOptions
	id           security.ID
	schema, name string
	deadline     time.Time
}

func (r *readerStub) begin(ctx context.Context, id security.ID, schema, name string, options RowReadOptions) error {
	r.mu.Lock()
	r.calls++
	r.id = id
	r.schema = schema
	r.name = name
	r.options = options
	r.deadline, _ = ctx.Deadline()
	hook, err := r.onRead, r.err
	r.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	return err
}

func (r *readerStub) Schemas(ctx context.Context, id security.ID) ([]Schema, error) {
	return r.schemas, r.begin(ctx, id, "", "", RowReadOptions{})
}
func (r *readerStub) Table(ctx context.Context, id security.ID, schema, name string) (Table, error) {
	return r.table, r.begin(ctx, id, schema, name, RowReadOptions{})
}
func (r *readerStub) Rows(ctx context.Context, id security.ID, schema, name string, options RowReadOptions) (RowBatch, error) {
	return r.batch, r.begin(ctx, id, schema, name, options)
}

type fixture struct {
	service   *Service
	auth      *authorizerStub
	directory *directoryStub
	reader    *readerStub
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	defaultValue := "'default'::text"
	auth := &authorizerStub{denied: map[security.ID]error{deniedID: security.ErrForbidden}}
	directory := &directoryStub{entries: []DatabaseSummary{{ID: databaseID, Name: "App", Access: security.AccessViewOnly}, {ID: deniedID, Name: "Hidden", Access: security.AccessViewEdit}}}
	reader := &readerStub{
		schemas: []Schema{
			{Name: "sales", Tables: []TableSummary{{Name: "orders", Kind: KindTable}}},
			{Name: "public", Tables: []TableSummary{{Name: "zeta", Kind: KindView}, {Name: "items", Kind: KindTable}}},
			{Name: "pg_catalog", Tables: []TableSummary{{Name: "pg_class", Kind: KindTable}}},
			{Name: "information_schema"}, {Name: "pg_toast"}, {Name: "pg_temp_1"},
		},
		table: Table{Schema: "public", Name: "items", Kind: KindTable,
			Columns:     []Column{{Name: "id", DataType: "integer"}, {Name: "note", DataType: "text", Nullable: true, Default: &defaultValue}},
			Constraints: []Constraint{{Name: "items_pkey", Type: "primary_key", Definition: "PRIMARY KEY (id)"}},
			Indexes:     []Index{{Name: "items_pkey", Definition: "CREATE UNIQUE INDEX items_pkey ON public.items (id)", Unique: true, Primary: true}},
		},
		batch: RowBatch{Columns: []string{"id", "note"}, Rows: [][]Cell{
			{{Text: "1"}, {Text: ""}}, {{Text: "2"}, {Null: true}}, {{Text: "3"}, {Text: "next page"}},
		}, Order: OrderPrimaryKey},
	}
	service, err := NewService(Config{Directory: directory, Reader: reader, Authorizer: auth})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{service, auth, directory, reader}
}

func requireError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}

func TestListAccessibleFiltersDeniedAndRevalidatesSession(t *testing.T) {
	f := newFixture(t)
	items, err := f.service.ListAccessible(context.Background(), principal())
	if err != nil || len(items) != 1 || items[0].ID != databaseID || items[0].Access != security.AccessViewOnly {
		t.Fatal("unauthorized metadata exposed or effective access lost")
	}
	if f.reader.calls != 0 {
		t.Fatal("listing opened a target database")
	}
	f.auth.err = security.ErrUnauthenticated
	_, err = f.service.ListAccessible(context.Background(), principal())
	requireError(t, err, security.ErrUnauthenticated)
	if f.directory.calls != 1 {
		t.Fatal("invalid session reached metadata directory")
	}
	f = newFixture(t)
	f.directory.entries = nil
	empty, err := f.service.ListAccessible(context.Background(), principal())
	encoded, _ := json.Marshal(empty)
	if err != nil || string(encoded) != "[]" {
		t.Fatal("empty database list must be []")
	}
}

func TestSchemasIncludeTablesButHideSystemSchemas(t *testing.T) {
	f := newFixture(t)
	schemas, err := f.service.Schemas(context.Background(), request())
	if err != nil || len(schemas) != 2 || schemas[0].Name != "public" || schemas[0].Tables[0].Name != "items" || schemas[1].Name != "sales" {
		t.Fatal("catalog sorting/filtering incorrect")
	}
	if f.reader.schemas[0].Name != "sales" || f.reader.schemas[1].Tables[0].Name != "zeta" {
		t.Fatal("service mutated adapter-owned catalog")
	}
	if f.reader.id != databaseID || f.reader.deadline.IsZero() || time.Until(f.reader.deadline) > ReadTimeout {
		t.Fatal("target ID/deadline not forwarded")
	}
	for _, action := range f.auth.actions {
		if action != security.ActionViewSchema {
			t.Fatal("wrong catalog permission")
		}
	}
}

func TestTableReturnsColumnsConstraintsIndexesAndIndependentDefaults(t *testing.T) {
	f := newFixture(t)
	table, err := f.service.Table(context.Background(), request(), "public", "items")
	if err != nil || len(table.Columns) != 2 || len(table.Constraints) != 1 || len(table.Indexes) != 1 || !table.Indexes[0].Unique ||
		!table.Columns[1].Nullable || table.Columns[0].Default != nil || *table.Columns[1].Default != "'default'::text" {
		t.Fatal("required catalog fields missing")
	}
	*table.Columns[1].Default = "changed"
	if *f.reader.table.Columns[1].Default != "'default'::text" {
		t.Fatal("table DTO aliases adapter state")
	}
}

func TestRowsPaginationAndNullAreDistinctFromEmptyText(t *testing.T) {
	f := newFixture(t)
	page, err := f.service.Rows(context.Background(), request(), RowsQuery{Schema: "public", Table: "items", Page: 2, Limit: 2})
	if err != nil || len(page.Rows) != 2 || !page.HasNext || page.Page != 2 || page.Limit != 2 || page.Order != OrderPrimaryKey {
		t.Fatal("pagination result invalid")
	}
	if f.reader.options.Offset != 2 || f.reader.options.FetchLimit != 3 || f.reader.options.MaxCellBytes != MaxCellBytes || f.reader.options.MaxPayloadBytes != MaxPayloadBytes {
		t.Fatal("adapter did not receive bounded fetch policy")
	}
	if page.Rows[0][1].Null || page.Rows[0][1].Text != "" || !page.Rows[1][1].Null {
		t.Fatal("NULL and empty string collapsed")
	}
	for _, action := range f.auth.actions {
		if action != security.ActionViewData {
			t.Fatal("wrong data permission")
		}
	}
	f.reader.batch = RowBatch{Columns: []string{"id"}}
	page, err = f.service.Rows(context.Background(), request(), RowsQuery{Schema: "public", Table: "items"})
	encoded, _ := json.Marshal(page)
	if err != nil || page.Page != 1 || page.Limit != 50 || page.HasNext || page.Order != OrderBestEffort || !strings.Contains(string(encoded), `"rows":[]`) {
		t.Fatal("empty/default page invalid")
	}
}

func TestDeniedAccessNeverCallsTargetReader(t *testing.T) {
	for _, operation := range []string{"schemas", "table", "rows"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t)
			req := request()
			req.DatabaseID = deniedID
			var err error
			switch operation {
			case "schemas":
				_, err = f.service.Schemas(context.Background(), req)
			case "table":
				_, err = f.service.Table(context.Background(), req, "public", "items")
			case "rows":
				_, err = f.service.Rows(context.Background(), req, RowsQuery{Schema: "public", Table: "items"})
			}
			requireError(t, err, security.ErrForbidden)
			if f.reader.calls != 0 {
				t.Fatal("unauthorized request opened target reader")
			}
		})
	}
}

func TestBadInputAndSystemSchemasNeverReachReader(t *testing.T) {
	f := newFixture(t)
	for _, query := range []RowsQuery{
		{Schema: "public", Table: "items", Page: -1}, {Schema: "public", Table: "items", Limit: 201},
		{Schema: "public", Table: "items", Page: int(^uint(0) >> 1)}, {Schema: "", Table: "items"}, {Schema: "public", Table: "bad\x00name"},
	} {
		_, err := f.service.Rows(context.Background(), request(), query)
		requireError(t, err, security.ErrInvalidInput)
	}
	_, err := f.service.Table(context.Background(), request(), "pg_catalog", "pg_class")
	requireError(t, err, security.ErrNotFound)
	_, err = f.service.Rows(context.Background(), request(), RowsQuery{Schema: "information_schema", Table: "tables"})
	requireError(t, err, security.ErrNotFound)
	if f.reader.calls != 0 {
		t.Fatal("invalid or internal schema request reached target reader")
	}
	req := request()
	req.Principal = security.Principal{}
	_, err = f.service.Schemas(context.Background(), req)
	requireError(t, err, security.ErrUnauthenticated)
	req = request()
	req.DatabaseID = "bad"
	_, err = f.service.Schemas(context.Background(), req)
	requireError(t, err, security.ErrForbidden)
}

func TestReadFailuresAreSanitizedAndNotSuccessfulEmptyResults(t *testing.T) {
	for _, failure := range []error{errors.New("postgres://user:private-password@host/db"), security.ErrUnavailable, security.ErrNotFound, context.DeadlineExceeded} {
		f := newFixture(t)
		f.reader.err = failure
		result, err := f.service.Rows(context.Background(), request(), RowsQuery{Schema: "public", Table: "items"})
		want := failure
		if strings.Contains(failure.Error(), "postgres://") {
			want = security.ErrUnavailable
		}
		requireError(t, err, want)
		if result.Columns != nil || strings.Contains(err.Error(), "private-password") {
			t.Fatal("read failure leaked data or driver details")
		}
	}
}

func TestRevocationDuringReadDiscardsResults(t *testing.T) {
	f := newFixture(t)
	f.reader.onRead = func(context.Context) {
		f.auth.mu.Lock()
		f.auth.denied[databaseID] = security.ErrForbidden
		f.auth.mu.Unlock()
	}
	result, err := f.service.Rows(context.Background(), request(), RowsQuery{Schema: "public", Table: "items"})
	requireError(t, err, security.ErrForbidden)
	if result.Columns != nil || result.Rows != nil {
		t.Fatal("data returned after access was revoked")
	}
}

func TestCancellationIsForwardedWithoutPartialOutput(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.service.Schemas(ctx, request())
	requireError(t, err, context.Canceled)
	if f.reader.calls != 0 {
		t.Fatal("canceled request reached target reader")
	}
	ctx, cancel = context.WithCancel(context.Background())
	f.reader.onRead = func(context.Context) { cancel() }
	result, err := f.service.Rows(ctx, request(), RowsQuery{Schema: "public", Table: "items"})
	requireError(t, err, context.Canceled)
	if result.Rows != nil {
		t.Fatal("canceled read returned partial data")
	}
}

func TestPayloadBudgetAndMalformedAdapterOutputFailClosed(t *testing.T) {
	for _, scenario := range []string{"budget", "width", "extra rows", "duplicate columns", "encoding", "wrong table", "catalog budget", "duplicate schemas", "unknown access"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			want := security.ErrUnavailable
			switch scenario {
			case "budget":
				f.reader.batch = RowBatch{Columns: []string{"value"}, Rows: make([][]Cell, MaxLimit)}
				for i := range f.reader.batch.Rows {
					f.reader.batch.Rows[i] = []Cell{{Text: strings.Repeat("&", MaxCellBytes)}}
				}
				want = security.ErrResultTooLarge
			case "width":
				f.reader.batch.Rows[0] = nil
			case "extra rows":
				f.reader.batch.Rows = make([][]Cell, MaxLimit+2)
			case "duplicate columns":
				f.reader.batch.Columns = []string{"id", "id"}
			case "encoding":
				f.reader.batch.Rows[0][0] = Cell{Text: "bad", Encoding: "executable"}
			case "wrong table":
				f.reader.table.Schema = "other"
			case "catalog budget":
				f.reader.table.Constraints[0].Definition = strings.Repeat("x", MaxPayloadBytes+1)
				want = security.ErrResultTooLarge
			case "duplicate schemas":
				f.reader.schemas = append(f.reader.schemas, f.reader.schemas[0])
			case "unknown access":
				f.directory.entries[0].Access = "owner"
			}
			var err error
			switch scenario {
			case "wrong table", "catalog budget":
				_, err = f.service.Table(context.Background(), request(), "public", "items")
			case "duplicate schemas":
				_, err = f.service.Schemas(context.Background(), request())
			case "unknown access":
				_, err = f.service.ListAccessible(context.Background(), principal())
			default:
				_, err = f.service.Rows(context.Background(), request(), RowsQuery{Schema: "public", Table: "items", Limit: MaxLimit})
			}
			requireError(t, err, want)
		})
	}
}

func TestConcurrentReadsDoNotMutateSharedAdapterSnapshots(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.service.Schemas(context.Background(), request()); err != nil {
				t.Error(err)
			}
			if _, err := f.service.Rows(context.Background(), request(), RowsQuery{Schema: "public", Table: "items"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.reader.schemas[0].Name != "sales" || f.reader.batch.Rows[0][1].Encoding != "" {
		t.Fatal("shared adapter snapshots were mutated")
	}
}

func TestExplorerConstructorRejectsMissingAndTypedNilDependencies(t *testing.T) {
	f := newFixture(t)
	if _, err := NewService(Config{}); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("missing ports accepted")
	}
	var reader *readerStub
	if _, err := NewService(Config{Directory: f.directory, Reader: reader, Authorizer: f.auth}); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("typed nil reader accepted")
	}
}
