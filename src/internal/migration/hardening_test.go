package migration_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type lostClaimAcknowledgement struct{ migration.Repository }

func (r lostClaimAcknowledgement) Reserve(ctx context.Context, req migration.Request, claim migration.Claim) (migration.Migration, error) {
	if _, err := r.Repository.Reserve(ctx, req, claim); err != nil {
		return migration.Migration{}, err
	}
	return migration.Migration{}, fmt.Errorf("CLAIM_PRIVATE_CANARY: %w", migration.ErrExecutionUncertain)
}

func TestLostReservationAcknowledgementPreservesUncertainty(t *testing.T) {
	original, fixture, auth := setup(t)
	id := draft(t, original)
	service, err := migration.NewService(migration.Config{Repository: lostClaimAcknowledgement{fixture}, Coordinator: fixture, Validator: migration.NewPostgreSQLValidator(), Authorizer: auth, Accounts: auth, IDs: security.NewTokenGenerator(), Clock: security.SystemClock{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(context.Background(), req, id, 1)
	if !errors.Is(err, migration.ErrExecutionUncertain) || strings.Contains(err.Error(), "CLAIM_PRIVATE_CANARY") {
		t.Fatal("claim uncertainty was lost or driver details leaked")
	}
	record, err := fixture.Find(context.Background(), db, id)
	if err != nil || record.Status != migration.Pending || fixture.Snapshot().Executes != 0 {
		t.Fatal("lost acknowledgement executed SQL or removed the pending claim")
	}
	if _, err := service.Execute(context.Background(), req, id, 1); !errors.Is(err, migration.ErrExecutionUncertain) || fixture.Snapshot().Executes != 0 {
		t.Fatal("pending claim allowed a blind retry")
	}
}
