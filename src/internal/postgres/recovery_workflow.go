package postgres

import (
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var _ migration.RecoveryWorkflowTx = (*workflowTx)(nil)

func (t *workflowTx) RecoverySource(id security.ID) (migration.JobView, *migration.ExecutionPlan, error) {
	job, err := findJob(t.ctx, t.tx, t.database(), id, " FOR UPDATE OF j")
	if err != nil {
		return migration.JobView{}, nil, err
	}
	var raw []byte
	if err := t.tx.QueryRow(t.ctx, "SELECT execution_plan FROM jobs WHERE database_id = $1 AND id = $2",
		string(t.database()), string(id)).Scan(&raw); err != nil {
		return migration.JobView{}, nil, mapError(err)
	}
	plan, err := decodePlan(t.ctx, t.tx, raw)
	return job, plan, err
}
