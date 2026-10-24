package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	testSessionID security.ID = "5ed48df2-bcdd-4b52-b9ad-8d9f6689df55"
	testDatabaseA security.ID = "3f1c2d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	testDatabaseB security.ID = "6a7b8c9d-0e1f-4a2b-9c3d-4e5f6a7b8c9d"
	testDatabaseC security.ID = "9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a"
)

var testPrincipal = security.Principal{UserID: testUserID, SessionID: testSessionID, AuthVersion: 1, CSRFEpoch: 1}

type fakeAuthorizer struct {
	adminErr, databaseErr error
	adminCalls            int
	databaseCalls         int
	principal             security.Principal
	databaseID            security.ID
	action                security.DatabaseAction
	denied                map[security.DatabaseAction]bool
}

func (a *fakeAuthorizer) RequirePlatformAdmin(_ context.Context, principal security.Principal) error {
	a.adminCalls++
	a.principal = principal
	return a.adminErr
}

func (a *fakeAuthorizer) RequireDatabase(_ context.Context, principal security.Principal, databaseID security.ID, action security.DatabaseAction) error {
	a.databaseCalls++
	a.principal, a.databaseID, a.action = principal, databaseID, action
	if a.denied[action] {
		return security.ErrForbidden
	}
	return a.databaseErr
}

func authorizeRequest(t *testing.T, guard *DatabaseGuard, target string, principal bool, action security.DatabaseAction) (DatabaseRequest, error) {
	t.Helper()
	var got DatabaseRequest
	var gotErr error
	mux := http.NewServeMux()
	mux.HandleFunc("GET /databases/{databaseID}/schema", func(_ http.ResponseWriter, r *http.Request) {
		got, gotErr = guard.Authorize(r, action)
	})
	r := httptest.NewRequest("GET", target, nil)
	if principal {
		r = r.WithContext(security.WithPrincipal(r.Context(), testPrincipal))
	}
	mux.ServeHTTP(httptest.NewRecorder(), r)
	return got, gotErr
}

func TestNewDatabaseGuardRejectsMissingAuthorizer(t *testing.T) {
	var nilAuthorizer *fakeAuthorizer
	for _, authorizer := range []security.Authorizer{nil, nilAuthorizer} {
		if guard, err := NewDatabaseGuard(authorizer); guard != nil || !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("missing authorizer accepted")
		}
	}
}

func TestAuthorizePassesPrincipalFromContextAndValidatedID(t *testing.T) {
	authorizer := &fakeAuthorizer{}
	guard, _ := NewDatabaseGuard(authorizer)
	got, err := authorizeRequest(t, guard, "/databases/"+string(testDatabaseA)+"/schema", true, security.ActionViewSchema)
	if err != nil || got.Principal != testPrincipal || got.DatabaseID != testDatabaseA {
		t.Fatal("authorized request lost principal or database id")
	}
	if authorizer.databaseCalls != 1 || authorizer.principal != testPrincipal || authorizer.databaseID != testDatabaseA || authorizer.action != security.ActionViewSchema {
		t.Fatal("authorizer called with wrong arguments")
	}
}

func TestAuthorizeRejectsBeforeCallingAuthorizer(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		target    string
		principal bool
		want      error
	}{
		{"no principal", "/databases/" + string(testDatabaseA) + "/schema", false, security.ErrUnauthenticated},
		{"malformed id", "/databases/not-a-uuid/schema", true, security.ErrInvalidInput},
		{"uppercase id", "/databases/3F1C2D4E-5A6B-4C7D-8E9F-0A1B2C3D4E5F/schema", true, security.ErrInvalidInput},
		{"zero id", "/databases/00000000-0000-0000-0000-000000000000/schema", true, security.ErrInvalidInput},
	} {
		authorizer := &fakeAuthorizer{}
		guard, _ := NewDatabaseGuard(authorizer)
		got, err := authorizeRequest(t, guard, scenario.target, scenario.principal, security.ActionViewSchema)
		if !errors.Is(err, scenario.want) || got != (DatabaseRequest{}) || authorizer.databaseCalls != 0 {
			t.Fatal("invalid request reached authorizer: " + scenario.name)
		}
	}
}

func TestAuthorizePropagatesDenialWithoutResult(t *testing.T) {
	for _, denial := range []error{security.ErrForbidden, security.ErrUnauthenticated, security.ErrUnavailable} {
		guard, _ := NewDatabaseGuard(&fakeAuthorizer{databaseErr: denial})
		got, err := authorizeRequest(t, guard, "/databases/"+string(testDatabaseA)+"/schema", true, security.ActionExecuteMigration)
		if !errors.Is(err, denial) || got != (DatabaseRequest{}) {
			t.Fatal("denial was not propagated or returned data")
		}
	}
}

func TestRequireAdmin(t *testing.T) {
	authorizer := &fakeAuthorizer{}
	guard, _ := NewDatabaseGuard(authorizer)
	r := httptest.NewRequest("GET", "/admin/users", nil)
	if _, err := guard.RequireAdmin(r); !errors.Is(err, security.ErrUnauthenticated) || authorizer.adminCalls != 0 {
		t.Fatal("admin check ran without principal")
	}
	r = r.WithContext(security.WithPrincipal(r.Context(), testPrincipal))
	if principal, err := guard.RequireAdmin(r); err != nil || principal != testPrincipal || authorizer.adminCalls != 1 {
		t.Fatal("admin rejected or principal lost")
	}
	authorizer.adminErr = security.ErrForbidden
	if principal, err := guard.RequireAdmin(r); !errors.Is(err, security.ErrForbidden) || principal != (security.Principal{}) {
		t.Fatal("non-admin accepted")
	}
}
