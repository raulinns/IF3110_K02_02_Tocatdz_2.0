package migration

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type DefinitionPlanOptions struct {
	PlanOptions
	Direction ExecutionDirection
}

type DefinitionPlanConfig struct {
	Definitions DefinitionStore
	Authorizer  security.Authorizer
	Coordinator PlanCoordinator
	Validator   PlanValidator
}

type DefinitionPlanService struct{ deps DefinitionPlanConfig }

func NewDefinitionPlanService(cfg DefinitionPlanConfig) (*DefinitionPlanService, error) {
	for _, dependency := range []any{cfg.Definitions, cfg.Authorizer, cfg.Coordinator, cfg.Validator} {
		if missing(dependency) {
			return nil, security.ErrInvalidInput
		}
	}
	return &DefinitionPlanService{deps: cfg}, nil
}

// AnalyzeDefinition plans one statement from a saved normal-mode definition.
// The shared analyzer rechecks grants and the definition while holding the target lock.
func (s *DefinitionPlanService) AnalyzeDefinition(ctx context.Context, req Request, id security.ID, options DefinitionPlanOptions) (PlanResult, error) {
	ctx, cancel := context.WithTimeout(ctx, PlanTimeout)
	defer cancel()
	authorize := func(ctx context.Context) error {
		return safeError(s.deps.Authorizer.RequireDatabase(ctx, req.Principal, req.DatabaseID, security.ActionAnalyzeMigration))
	}
	if err := authorize(ctx); err != nil {
		return PlanResult{}, err
	}
	if !security.ValidID(req.DatabaseID) || !security.ValidID(id) || options.Revision == 0 || options.Revision > (1<<53)-1 ||
		options.Statement < 1 || options.Statement > 1000 ||
		(options.Direction != DirectionUp && options.Direction != DirectionDown) {
		return PlanResult{}, security.ErrInvalidInput
	}
	load := func(ctx context.Context) (planSource, error) {
		definition, err := s.deps.Definitions.FindDefinition(ctx, req.DatabaseID, id)
		if err != nil {
			return planSource{}, err
		}
		if definition.ID != id || definition.DatabaseID != req.DatabaseID ||
			(definition.State != DefinitionDraft && definition.State != DefinitionFrozen) {
			return planSource{}, security.ErrUnavailable
		}
		if definition.Revision != options.Revision {
			return planSource{}, security.ErrConflict
		}
		if definition.Checksum != ChecksumDefinition(definition.DefinitionInput) {
			return planSource{}, security.ErrUnavailable
		}
		if definition.Mode != ModeNormal {
			return planSource{}, PlanRejection("batch")
		}
		sql := definition.UpSQL
		if options.Direction == DirectionDown {
			if definition.Irreversible || definition.DownSQL.Empty() {
				return planSource{}, PlanRejection("direction")
			}
			sql = definition.DownSQL
		}
		return planSource{sql: sql, revision: definition.Revision, checksum: definition.Checksum, state: definition.State}, nil
	}
	return analyzeStoredPlan(ctx, req.DatabaseID, options.PlanOptions, s.deps.Coordinator, s.deps.Validator, authorize, load)
}
