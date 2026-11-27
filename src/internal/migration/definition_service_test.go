package migration

import (
	"context"
	"errors"
	"fmt"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"testing"
	"time"
)

func normalDefinition() DefinitionInput {
	return DefinitionInput{Title: "Create example table", UpSQL: security.NewSecret("CREATE TABLE public.contract_example (id bigint PRIMARY KEY);"), DownSQL: security.NewSecret("DROP TABLE public.contract_example;"), Mode: ModeNormal}
}

func batchDefinition() DefinitionInput {
	return DefinitionInput{Title: "Batch", UpSQL: security.NewSecret("enabled = true"), DownSQL: security.NewSecret("enabled = false"), Mode: ModeBatch,
		Batch: &BatchConfig{Schema: "public", Table: "items", KeyColumn: "id", BatchSize: MinBatchSize}}
}

func TestDefinitionValidationAndCanonicalChecksum(t *testing.T) {
	input := normalDefinition()
	if got := fmt.Sprintf("%x", ChecksumDefinition(input)); got != "2d1d2608de76d78f25ad2ba34577509d51ae6aee9f41127db71762e27c69d443" {
		t.Fatal("checksum differs from the checksum fixture", got)
	}
	if err := ValidateDefinition(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDefinition(context.Background(), batchDefinition()); err != nil {
		t.Fatalf("valid batch: %v", err)
	}
	for _, test := range []struct {
		name string
		edit func(*DefinitionInput)
	}{
		{"missing down", func(i *DefinitionInput) { i.DownSQL = security.Secret{} }},
		{"bad down", func(i *DefinitionInput) { i.DownSQL = security.NewSecret("COMMIT") }},
		{"irreversible with down", func(i *DefinitionInput) { i.Irreversible = true; i.IrreversibleReason = "no restore" }},
		{"bad batch size", func(i *DefinitionInput) { *i = batchDefinition(); i.Batch.BatchSize = MinBatchSize - 1 }},
		{"key mutation", func(i *DefinitionInput) { *i = batchDefinition(); i.DownSQL = security.NewSecret(`"id" = 2`) }},
		{"clause injection", func(i *DefinitionInput) {
			*i = batchDefinition()
			i.UpSQL = security.NewSecret("enabled = true FROM other")
		}},
		{"trailing comment", func(i *DefinitionInput) {
			*i = batchDefinition()
			i.UpSQL = security.NewSecret("enabled = true -- comment")
		}},
		{"unsupported predicate", func(i *DefinitionInput) { *i = batchDefinition(); i.Batch.ScopeWhere = security.NewSecret("id > 1") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			i := normalDefinition()
			test.edit(&i)
			if err := ValidateDefinition(context.Background(), i); !errors.Is(err, security.ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
	input.Irreversible = true
	input.DownSQL = security.Secret{}
	input.IrreversibleReason = "No inverse"
	if err := ValidateDefinition(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if ChecksumDefinition(input) == ChecksumDefinition(normalDefinition()) {
		t.Fatal("checksum ignored definition change")
	}
}

type draftTestStore struct {
	WorkflowTx
	stored MigrationDefinition
	access security.DatabaseAccess
	calls  int
}

func (s *draftTestStore) WithWorkflow(_ context.Context, _ Request, fn func(WorkflowTx) error) error {
	s.calls++
	return fn(s)
}
func (s *draftTestStore) RequireAccess(_ security.DatabaseAccess) error {
	if s.access != security.AccessViewEdit {
		return security.ErrForbidden
	}
	return nil
}
func (s *draftTestStore) CreateDefinition(d MigrationDefinition) error { s.stored = d; return nil }
func (s *draftTestStore) UpdateDefinition(id security.ID, revision uint64, input DefinitionInput, sum DefinitionChecksum, at time.Time) (MigrationDefinition, error) {
	if s.stored.ID != id || s.stored.Revision != revision || s.stored.State != DefinitionDraft {
		return MigrationDefinition{}, security.ErrConflict
	}
	s.stored.Revision++
	s.stored.DefinitionInput = input
	s.stored.Checksum = sum
	s.stored.UpdatedAt = at
	return s.stored, nil
}

type draftTestAuth struct {
	security.Authorizer
	access security.DatabaseAccess
}

func (a *draftTestAuth) RequireDatabase(_ context.Context, _ security.Principal, _ security.ID, action security.DatabaseAction) error {
	if !security.Allows(a.access, action) {
		return security.ErrForbidden
	}
	return nil
}

func TestDefinitionServiceRequiresWriteAndPreservesCAS(t *testing.T) {
	ctx := context.Background()
	store := &draftTestStore{access: security.AccessViewEdit}
	auth := &draftTestAuth{access: security.AccessViewOnly}
	ids := security.NewTokenGenerator()
	s, err := NewDefinitionService(store, auth, ids, security.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{DatabaseID: rvID(1), Principal: security.Principal{UserID: rvID(2)}}
	if _, err := s.CreateDefinition(ctx, req, normalDefinition()); !errors.Is(err, security.ErrForbidden) || store.calls != 0 {
		t.Fatal("viewer reached draft transaction", err)
	}
	auth.access = security.AccessViewEdit
	store.access = security.AccessViewOnly
	if _, err := s.CreateDefinition(ctx, req, normalDefinition()); !errors.Is(err, security.ErrForbidden) || store.stored.ID != "" {
		t.Fatal("revoked access reached write", err)
	}
	store.access = security.AccessViewEdit
	draft, err := s.CreateDefinition(ctx, req, normalDefinition())
	if err != nil || draft.Revision != 1 || draft.CreatedBy != req.Principal.UserID || draft.Checksum != ChecksumDefinition(normalDefinition()) {
		t.Fatal("bad draft", err)
	}
	input := normalDefinition()
	input.Title = "Updated"
	updated, err := s.UpdateDefinition(ctx, req, draft.ID, 1, input)
	if err != nil || updated.Revision != 2 || updated.Checksum != ChecksumDefinition(input) {
		t.Fatal("bad update", err)
	}
	if _, err := s.UpdateDefinition(ctx, req, draft.ID, 1, input); !errors.Is(err, security.ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
	store.stored.State = DefinitionFrozen
	if _, err := s.UpdateDefinition(ctx, req, draft.ID, 2, input); !errors.Is(err, security.ErrConflict) {
		t.Fatal("frozen definition changed", err)
	}
}
