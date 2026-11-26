package migration

import "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"

// RecoveryWorkflowTx reads source job and its immutable plan under the same
// database guard as the new change request and enqueue operation.
type RecoveryWorkflowTx interface {
	RecoverySource(security.ID) (JobView, *ExecutionPlan, error)
}

func recoverySource(tx WorkflowTx, id security.ID) (JobView, *ExecutionPlan, error) {
	reader, ok := tx.(RecoveryWorkflowTx)
	if !ok {
		return JobView{}, nil, security.ErrUnavailable
	}
	return reader.RecoverySource(id)
}

func checkRecoverySubmission(tx WorkflowTx, position Position, change ChangeRequest, definition MigrationDefinition) error {
	if position.BlockedByJobID != change.SourceJobID || !security.ValidID(change.SourceJobID) {
		return security.ErrConflict
	}
	source, original, err := recoverySource(tx, change.SourceJobID)
	if err != nil {
		return err
	}
	if source.ID != change.SourceJobID || source.DatabaseID != change.DatabaseID ||
		source.MigrationID != definition.ID || definition.Mode != ModeBatch ||
		definition.Checksum != change.Checksum || definition.Revision != change.MigrationRevision ||
		ChecksumDefinition(definition.DefinitionInput) != definition.Checksum {
		return security.ErrConflict
	}
	receipt := source.ExecutionID
	if change.Kind == ChangeRevertBatches {
		// The new compensation will allocate its own receipt. Its temporary
		// identity here is only used to validate the original lineage.
		receipt = change.ID
	}
	snapshot := RevalidationSnapshot{
		Job:           JobView{JobSpec: JobSpec{ID: change.ID, DatabaseID: change.DatabaseID}},
		ChangeRequest: change, SourceJob: &source, SourcePlan: original,
	}
	plan := ExecutionPlan{
		DatabaseID: change.DatabaseID, MigrationID: definition.ID, Definition: definition,
		Kind: change.Kind, SourceJobID: change.SourceJobID, ReceiptExecutionID: receipt,
	}
	if !applyRecoverySource(snapshot, &plan) {
		return security.ErrConflict
	}
	return nil
}
