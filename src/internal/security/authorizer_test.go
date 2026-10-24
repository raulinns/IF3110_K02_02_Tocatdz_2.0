package security

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackendAuthorizerPermissionMatrix(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	authorizer, err := NewBackendAuthorizer(f.store, f.store, f.clock, DefaultSessionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	assertAuthError(t, authorizer.RequirePlatformAdmin(ctx, result.Principal), ErrForbidden)
	assertAuthError(t, authorizer.RequireDatabase(ctx, result.Principal, otherID, ActionViewData), ErrForbidden)
	for _, grants := range [][]DatabaseAccess{{AccessViewOnly}, {AccessViewOnly, AccessViewEdit}, {AccessViewEdit, AccessViewOnly}} {
		f.store.grants[otherID] = grants
		if err := authorizer.RequireDatabase(ctx, result.Principal, otherID, ActionViewSchema); err != nil {
			t.Fatal(err)
		}
		wantEdit := len(grants) == 2
		err := authorizer.RequireDatabase(ctx, result.Principal, otherID, ActionExecuteMigration)
		if wantEdit && err != nil || !wantEdit && !errors.Is(err, ErrForbidden) {
			t.Fatal("incorrect effective group access")
		}
	}
	record := f.store.accounts[testUserID]
	record.Account.PlatformAdmin = true
	f.store.accounts[testUserID] = record
	if err := authorizer.RequirePlatformAdmin(ctx, result.Principal); err != nil {
		t.Fatal(err)
	}
	f.store.grants[otherID] = nil
	assertAuthError(t, authorizer.RequireDatabase(ctx, result.Principal, otherID, ActionExecuteMigration), ErrForbidden)
}

func TestBackendAuthorizerRevalidatesSessionsAndFailsClosed(t *testing.T) {
	for _, scenario := range []string{"revoked", "idle", "account epoch", "principal user", "principal epoch", "session missing", "outage", "grant outage", "unknown level"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAuthFixture(t)
			result := f.login(t)
			principal := result.Principal
			authorizer, _ := NewBackendAuthorizer(f.store, f.store, f.clock, DefaultSessionPolicy())
			f.store.grants[otherID] = []DatabaseAccess{AccessViewEdit}
			want := ErrUnauthenticated
			switch scenario {
			case "revoked":
				_ = f.store.Revoke(context.Background(), principal.SessionID, f.clock.Now())
			case "idle":
				f.clock.add(30 * time.Minute)
			case "account epoch":
				record := f.store.accounts[testUserID]
				record.Account.AuthVersion++
				f.store.accounts[testUserID] = record
			case "principal user":
				principal.UserID = otherID
			case "principal epoch":
				principal.CSRFEpoch++
			case "session missing":
				delete(f.store.sessions, principal.SessionID)
			case "outage":
				f.store.lookupErr = errors.New("private DSN")
				want = ErrUnavailable
			case "grant outage":
				f.store.grantErr = errors.New("private query")
				want = ErrUnavailable
			case "unknown level":
				f.store.grants[otherID] = []DatabaseAccess{"owner"}
				want = ErrUnavailable
			}
			assertAuthError(t, authorizer.RequireDatabase(context.Background(), principal, otherID, ActionExecuteMigration), want)
		})
	}
}

func TestBackendAuthorizerRejectsUnknownActionsBeforeGrantQuery(t *testing.T) {
	f := newAuthFixture(t)
	result := f.login(t)
	authorizer, _ := NewBackendAuthorizer(f.store, f.store, f.clock, DefaultSessionPolicy())
	for _, action := range []DatabaseAction{"", "database.drop", "admin"} {
		assertAuthError(t, authorizer.RequireDatabase(context.Background(), result.Principal, otherID, action), ErrForbidden)
	}
	assertAuthError(t, authorizer.RequireDatabase(context.Background(), result.Principal, "bad", ActionViewData), ErrForbidden)
	if f.store.grantCalls != 0 {
		t.Fatal("invalid action or ID reached grant query")
	}
	if _, err := NewBackendAuthorizer(nil, f.store, f.clock, DefaultSessionPolicy()); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing repository accepted")
	}
}
