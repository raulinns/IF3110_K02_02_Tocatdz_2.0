package security_test

import (
	"context"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestEffectiveAccess(t *testing.T) {
	tests := []struct {
		name   string
		grants []security.DatabaseAccess
		want   security.DatabaseAccess
		bad    bool
	}{
		{"no groups", nil, security.AccessNone, false},
		{"no grant", []security.DatabaseAccess{security.AccessNone}, security.AccessNone, false},
		{"view only", []security.DatabaseAccess{security.AccessViewOnly}, security.AccessViewOnly, false},
		{"edit then view", []security.DatabaseAccess{security.AccessViewEdit, security.AccessViewOnly}, security.AccessViewEdit, false},
		{"view then edit", []security.DatabaseAccess{security.AccessViewOnly, security.AccessViewEdit}, security.AccessViewEdit, false},
		{"unknown after edit", []security.DatabaseAccess{security.AccessViewEdit, "owner"}, security.AccessNone, true},
		{"empty level", []security.DatabaseAccess{""}, security.AccessNone, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := security.EffectiveAccess(tt.grants)
			if got != tt.want || errors.Is(err, security.ErrInvalidInput) != tt.bad {
				t.Fatalf("EffectiveAccess = %q, %v; want %q, invalid=%v", got, err, tt.want, tt.bad)
			}
		})
	}
}

func TestPermissionMatrix(t *testing.T) {
	readActions := []security.DatabaseAction{
		security.ActionViewSchema, security.ActionViewData,
		security.ActionViewHistory, security.ActionViewMigration,
	}
	writeActions := []security.DatabaseAction{
		security.ActionCreateMigration, security.ActionUpdateMigration, security.ActionExecuteMigration, security.ActionAnalyzeMigration,
	}
	for _, access := range []security.DatabaseAccess{security.AccessNone, security.AccessViewOnly, security.AccessViewEdit, "admin", ""} {
		for _, action := range append(append([]security.DatabaseAction{}, readActions...), writeActions...) {
			want := access == security.AccessViewEdit
			for _, read := range readActions {
				if action == read && access == security.AccessViewOnly {
					want = true
				}
			}
			if got := security.Allows(access, action); got != want {
				t.Errorf("Allows(%q, %q) = %v; want %v", access, action, got, want)
			}
		}
		for _, action := range []security.DatabaseAction{"", "admin", "migration.delete", "database.drop"} {
			if security.Allows(access, action) {
				t.Errorf("unknown action %q allowed for %q", action, access)
			}
		}
	}
}

func TestPrincipalContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := security.PrincipalFromContext(ctx); ok {
		t.Fatal("missing principal accepted")
	}
	principal := security.Principal{UserID: "user", SessionID: "family", AuthVersion: 1, CSRFEpoch: 1}
	for _, invalid := range []security.Principal{
		{}, {UserID: "user"}, {SessionID: "family"}, {UserID: "user", SessionID: "family"},
		{UserID: "user", SessionID: "family", AuthVersion: 1},
	} {
		if _, ok := security.PrincipalFromContext(security.WithPrincipal(ctx, invalid)); ok {
			t.Fatalf("incomplete principal accepted: %+v", invalid)
		}
	}
	got, ok := security.PrincipalFromContext(security.WithPrincipal(ctx, principal))
	if !ok || got != principal {
		t.Fatal("authenticated principal not propagated")
	}
}

func TestSessionPolicyValidation(t *testing.T) {
	p := security.DefaultSessionPolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []security.SessionPolicy{
		{},
		{IdleTimeout: p.IdleTimeout, AbsoluteLifetime: p.AbsoluteLifetime, RotationInterval: p.RotationInterval},
		{IdleTimeout: p.IdleTimeout, AbsoluteLifetime: p.AbsoluteLifetime, RotationInterval: p.RotationInterval, GracePeriod: -1},
		{IdleTimeout: p.IdleTimeout, AbsoluteLifetime: p.AbsoluteLifetime, RotationInterval: p.RotationInterval, GracePeriod: p.RotationInterval},
		{IdleTimeout: p.IdleTimeout, AbsoluteLifetime: p.AbsoluteLifetime, RotationInterval: p.IdleTimeout, GracePeriod: p.GracePeriod},
		{IdleTimeout: p.AbsoluteLifetime, AbsoluteLifetime: p.AbsoluteLifetime, RotationInterval: p.RotationInterval, GracePeriod: p.GracePeriod},
	}
	for _, policy := range invalid {
		if !errors.Is(policy.Validate(), security.ErrInvalidInput) {
			t.Errorf("invalid session deadlines accepted: %+v", policy)
		}
	}
}
