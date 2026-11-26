package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetbatch"
)

type checkpointOnlyTarget struct {
	targetbatch.Target
}

func (checkpointOnlyTarget) Checkpoints(_ context.Context, id security.ID, direction targetbatch.Direction, _ targetbatch.Query) ([]targetbatch.Checkpoint, error) {
	return []targetbatch.Checkpoint{{ExecutionID: id, Direction: direction, Batch: 1, To: 100, ScopeMax: 300, Rows: 100}}, nil
}

type recordingMirror struct {
	items []migration.BatchCheckpoint
	err   error
}

func (m *recordingMirror) BatchCommitted(_ context.Context, item migration.BatchCheckpoint) error {
	m.items = append(m.items, item)
	return m.err
}

func (m *recordingMirror) BatchReverted(ctx context.Context, item migration.BatchCheckpoint) error {
	return m.BatchCommitted(ctx, item)
}

func TestBatchMirrorRepairReadsEvidenceWithoutUpdatingRows(t *testing.T) {
	runner, err := NewBatchRunner(security.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	job := batchJob{
		target: checkpointOnlyTarget{}, direction: targetbatch.Up,
		config: migration.BatchConfig{Schema: "public", Table: "items", KeyColumn: "id", BatchSize: 100},
		plan:   migration.ExecutionPlan{Definition: migration.MigrationDefinition{DefinitionInput: migration.DefinitionInput{UpSQL: security.NewSecret("amount = amount + 1")}}},
		table:  "items", key: "id",
	}
	mirror := &recordingMirror{}
	if err := runner.repairMirrors(context.Background(), job, mirror); err != nil {
		t.Fatal(err)
	}
	if len(mirror.items) != 1 || mirror.items[0].Statement.Batch == nil || len(mirror.items[0].Statement.Params) != 3 ||
		mirror.items[0].Statement.Query.Empty() {
		t.Fatal("repair did not project the original checkpoint statement")
	}
	mirror.err = errors.New("unavailable")
	if err := runner.repairMirrors(context.Background(), job, mirror); !errors.Is(err, mirror.err) {
		t.Fatal("mirror failure must stop repair")
	}
}
