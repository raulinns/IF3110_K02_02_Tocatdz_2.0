package migration

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// DefinitionService manages M2 drafts via the platform transaction boundary.
type DefinitionService struct {
	store WorkflowStore
	auth  security.Authorizer
	ids   IDs
	clock security.Clock
}

func NewDefinitionService(store WorkflowStore, auth security.Authorizer, ids IDs, clock security.Clock) (*DefinitionService, error) {
	if missing(store) || missing(auth) || missing(ids) || missing(clock) {
		return nil, security.ErrInvalidInput
	}
	return &DefinitionService{store: store, auth: auth, ids: ids, clock: clock}, nil
}

func (s *DefinitionService) CreateDefinition(ctx context.Context, request Request, input DefinitionInput) (MigrationDefinition, error) {
	if err := s.auth.RequireDatabase(ctx, request.Principal, request.DatabaseID, security.ActionCreateMigration); err != nil {
		return MigrationDefinition{}, err
	}
	if err := ValidateDefinition(ctx, input); err != nil {
		return MigrationDefinition{}, err
	}
	id, err := s.ids.GenerateID()
	if err != nil || !security.ValidID(id) {
		return MigrationDefinition{}, security.ErrUnavailable
	}
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	draft := MigrationDefinition{ID: id, DatabaseID: request.DatabaseID, DefinitionInput: input, State: DefinitionDraft, Revision: 1,
		Checksum: ChecksumDefinition(input), CreatedBy: request.Principal.UserID, CreatedAt: at, UpdatedAt: at}
	err = s.store.WithWorkflow(ctx, request, func(tx WorkflowTx) error {
		if err := tx.RequireAccess(security.AccessViewEdit); err != nil {
			return err
		}
		return tx.CreateDefinition(draft)
	})
	if err != nil {
		return MigrationDefinition{}, safeError(err)
	}
	return draft, nil
}

func (s *DefinitionService) UpdateDefinition(ctx context.Context, request Request, id security.ID, revision uint64, input DefinitionInput) (MigrationDefinition, error) {
	if err := s.auth.RequireDatabase(ctx, request.Principal, request.DatabaseID, security.ActionUpdateMigration); err != nil {
		return MigrationDefinition{}, err
	}
	if !security.ValidID(id) || revision == 0 || revision >= (1<<53)-1 {
		return MigrationDefinition{}, security.ErrInvalidInput
	}
	if err := ValidateDefinition(ctx, input); err != nil {
		return MigrationDefinition{}, err
	}
	sum := ChecksumDefinition(input)
	var updated MigrationDefinition
	err := s.store.WithWorkflow(ctx, request, func(tx WorkflowTx) error {
		if err := tx.RequireAccess(security.AccessViewEdit); err != nil {
			return err
		}
		var err error
		updated, err = tx.UpdateDefinition(id, revision, input, sum, s.clock.Now().UTC().Truncate(time.Microsecond))
		return err
	})
	if err != nil {
		return MigrationDefinition{}, safeError(err)
	}
	return updated, nil
}
