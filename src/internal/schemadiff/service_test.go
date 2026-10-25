package schemadiff_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	currentDB security.ID = "3f1c2d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	desiredDB security.ID = "6a7b8c9d-0e1f-4a2b-9c3d-4e5f6a7b8c9d"
)

var principal = security.Principal{UserID: "11111111-1111-4111-8111-111111111111", SessionID: "22222222-2222-4222-8222-222222222222", AuthVersion: 1, CSRFEpoch: 1}

type fakeReader struct {
	snapshots map[security.ID]schemadiff.Snapshot
	calls     int
	after     func()
}

func (f *fakeReader) Snapshot(_ context.Context, id security.ID, schema string) (schemadiff.Snapshot, error) {
	f.calls++
	if f.after != nil {
		f.after()
	}
	snapshot, ok := f.snapshots[id]
	if !ok {
		return schemadiff.Snapshot{}, errors.New("driver detail canary-secret")
	}
	snapshot.Schema = schema
	return snapshot, nil
}

type fakeAuthorizer struct{ denied map[security.ID]bool }

func (fakeAuthorizer) RequireAuthenticated(context.Context, security.Principal) error { return nil }
func (fakeAuthorizer) RequirePlatformAdmin(context.Context, security.Principal) error { return nil }
func (a *fakeAuthorizer) RequireDatabase(_ context.Context, _ security.Principal, id security.ID, action security.DatabaseAction) error {
	if action != security.ActionViewSchema || a.denied[id] {
		return security.ErrForbidden
	}
	return nil
}

func setup(t *testing.T) (*schemadiff.Service, *fakeReader, *fakeAuthorizer) {
	t.Helper()
	reader := &fakeReader{snapshots: map[security.ID]schemadiff.Snapshot{
		currentDB: {Quoted: "public"},
		desiredDB: {Quoted: "public", Tables: []schemadiff.Table{{Name: "items", Quoted: "items", Columns: []schemadiff.Column{
			{Name: "created_at", Quoted: "created_at", Type: "timestamp with time zone", Default: ptr("now()")},
		}}}},
	}}
	auth := &fakeAuthorizer{denied: map[security.ID]bool{}}
	service, err := schemadiff.NewService(schemadiff.Config{Reader: reader, Validator: migration.NewPostgreSQLValidator(), Authorizer: auth})
	if err != nil {
		t.Fatal(err)
	}
	return service, reader, auth
}

func ptr(value string) *string { return &value }

func request() schemadiff.Request {
	return schemadiff.Request{Principal: principal, CurrentID: currentDB, CurrentSchema: "public", DesiredID: desiredDB, DesiredSchema: "public"}
}

func TestCompareQualifiesFunctionsAndValidates(t *testing.T) {
	service, _, _ := setup(t)
	result, err := service.Compare(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.SQL.Reveal(), "DEFAULT pg_catalog.now()") || result.Validation != "" || len(result.Changes) != 1 {
		t.Fatalf("candidate not ready for migration: %q %q", result.SQL.Reveal(), result.Validation)
	}
}

func TestCompareReportsValidatorRejectionWithoutFailing(t *testing.T) {
	service, reader, _ := setup(t)
	snapshot := reader.snapshots[desiredDB]
	snapshot.Tables[0].Columns[0].Default = ptr("md5('x'::text)")
	reader.snapshots[desiredDB] = snapshot
	result, err := service.Compare(context.Background(), request())
	if err != nil || result.Validation == "" || result.SQL.Empty() {
		t.Fatalf("rejected candidate should still be shown for editing: %v %q", err, result.Validation)
	}
}

func TestCompareRequiresSchemaAccessOnBothDatabases(t *testing.T) {
	service, reader, auth := setup(t)
	auth.denied[desiredDB] = true
	if _, err := service.Compare(context.Background(), request()); !errors.Is(err, security.ErrForbidden) || reader.calls != 0 {
		t.Fatal("desired database read without access")
	}
	auth.denied[desiredDB] = false
	reader.after = func() { auth.denied[currentDB] = true }
	if _, err := service.Compare(context.Background(), request()); !errors.Is(err, security.ErrForbidden) {
		t.Fatal("access revoked during compare not detected")
	}
}

func TestCompareRejectsInvalidInputAndHidesErrors(t *testing.T) {
	service, reader, _ := setup(t)
	for _, req := range []schemadiff.Request{
		{Principal: principal, CurrentID: currentDB, CurrentSchema: "pg_catalog", DesiredID: desiredDB, DesiredSchema: "public"},
		{Principal: principal, CurrentID: currentDB, CurrentSchema: "public", DesiredID: desiredDB, DesiredSchema: "tocatdz_internal"},
		{Principal: principal, CurrentID: currentDB, CurrentSchema: "public", DesiredID: currentDB, DesiredSchema: "public"},
		{Principal: principal, CurrentID: "bukan-id", CurrentSchema: "public", DesiredID: desiredDB, DesiredSchema: "public"},
		{Principal: principal, CurrentID: currentDB, CurrentSchema: strings.Repeat("a", 64), DesiredID: desiredDB, DesiredSchema: "public"},
	} {
		if _, err := service.Compare(context.Background(), req); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatalf("invalid request accepted: %+v %v", req, err)
		}
	}
	if _, err := service.Compare(context.Background(), schemadiff.Request{CurrentID: currentDB, CurrentSchema: "public", DesiredID: desiredDB, DesiredSchema: "public"}); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatal("anonymous compare allowed")
	}
	delete(reader.snapshots, desiredDB)
	if _, err := service.Compare(context.Background(), request()); !errors.Is(err, security.ErrUnavailable) || strings.Contains(err.Error(), "canary") {
		t.Fatalf("reader error leaked: %v", err)
	}
}

func TestCompareSameSchemaHasNoCandidate(t *testing.T) {
	service, reader, _ := setup(t)
	reader.snapshots[desiredDB] = reader.snapshots[currentDB]
	result, err := service.Compare(context.Background(), request())
	if err != nil || len(result.Changes) != 0 || !result.SQL.Empty() {
		t.Fatalf("equal schemas produced candidate: %+v %v", result, err)
	}
}
