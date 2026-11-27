package migration

import "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"

func validSubmissionState(kind ChangeKind, definition MigrationDefinition) bool {
	if kind == ChangeApply {
		return definition.State == DefinitionDraft && definition.FirstExecutionAt == nil
	}
	return definition.State == DefinitionFrozen
}

func checkSubmissionPosition(tx WorkflowTx, position Position, change ChangeRequest, definition MigrationDefinition) error {
	if change.RetryOfJobID != "" {
		if err := checkRetrySubmission(tx, change, definition); err != nil {
			return err
		}
	}
	if change.Kind == ChangeResume || change.Kind == ChangeRevertBatches {
		return checkRecoverySubmission(tx, position, change, definition)
	}
	if change.Kind == ChangeApply && change.SourceJobID != "" {
		source, _, err := recoverySource(tx, change.SourceJobID)
		if err != nil {
			return err
		}
		if position.BlockedByJobID != source.ID || source.ID != change.SourceJobID ||
			source.DatabaseID != change.DatabaseID || source.Mode != ModeBatch || source.MigrationID == definition.ID ||
			(source.Status != JobPartial && source.Status != JobPartiallyReverted) {
			return security.ErrConflict
		}
		return nil
	}
	if position.BlockedByJobID != "" {
		return security.ErrConflict
	}
	if change.Kind == ChangeApply {
		if len(position.Forward) != 0 {
			return security.ErrConflict
		}
		return nil
	}
	expected := historyTarget(position, change.Kind)
	if expected.ID != change.TargetExecutionID {
		return security.ErrConflict
	}
	target, err := tx.Execution(expected.ID)
	if err != nil {
		return err
	}
	return checkHistoryTarget(expected, target, definition, change.Kind)
}

// historyTarget selects one execution from the database's linear history.
func historyTarget(position Position, kind ChangeKind) CommittedExecutionSummary {
	if kind == ChangeRollback && len(position.Active) > 0 {
		return position.Active[len(position.Active)-1]
	}
	if kind == ChangeForward && len(position.Forward) > 0 {
		return position.Forward[0]
	}
	return CommittedExecutionSummary{}
}

// The snapshot identifies the exact definition approved for the original
// execution. Both submission and worker revalidation use the same rules.
func checkHistoryTarget(expected CommittedExecutionSummary, target Execution, definition MigrationDefinition, kind ChangeKind) error {
	if !security.ValidID(expected.ID) || target.ID != expected.ID ||
		target.DatabaseID != definition.DatabaseID || target.MigrationID != definition.ID ||
		target.MigrationRevision != definition.Revision || target.Checksum != definition.Checksum ||
		expected.DatabaseID != target.DatabaseID || expected.MigrationID != target.MigrationID ||
		expected.Checksum != target.Checksum || expected.MigrationRevision != target.MigrationRevision ||
		expected.Direction != DirectionUp || target.Direction != DirectionUp ||
		ChecksumDefinition(target.Definition) != target.Checksum {
		return security.ErrConflict
	}
	if kind == ChangeRollback && (target.Definition.Irreversible || target.Definition.DownSQL.Empty()) {
		return WorkflowIrreversible
	}
	return nil
}
