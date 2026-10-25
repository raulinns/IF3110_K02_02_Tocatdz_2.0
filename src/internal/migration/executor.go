package migration

import (
	"context"
	"math"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), CleanupTimeout)
}

func (s *Service) Execute(ctx context.Context, req Request, id security.ID, revision uint64) (ExecutionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, ExecutionTimeout)
	defer cancel()
	if err := s.authorize(ctx, req, security.ActionExecuteMigration); err != nil {
		return ExecutionResult{}, err
	}
	if !security.ValidID(id) || revision == 0 || revision > math.MaxUint64-2 {
		return ExecutionResult{}, security.ErrInvalidInput
	}
	var result ExecutionResult
	err := s.deps.Coordinator.WithDatabaseLock(ctx, req.DatabaseID, func(locked context.Context, target LockedTarget) error {
		if err := s.authorize(locked, req, security.ActionExecuteMigration); err != nil {
			return err
		}
		if missing(target) {
			return security.ErrUnavailable
		}
		record, err := s.find(locked, req, id)
		if err != nil {
			return err
		}
		if record.Status == Pending {
			result = resultFor(record)
			return ErrExecutionUncertain
		}
		if record.Status != DraftStatus || record.Revision != revision {
			return security.ErrConflict
		}
		var validationErr error
		if validator, ok := s.deps.Validator.(DatabaseValidator); ok {
			validationErr = validator.ValidateDatabase(locked, req.DatabaseID, record.SQL)
		} else {
			validationErr = s.deps.Validator.Validate(locked, record.SQL)
		}
		if validationErr != nil {
			return safeValidationError(validationErr)
		}
		account, err := s.deps.Accounts.FindAccount(locked, req.Principal.UserID)
		if err != nil {
			return safeError(err)
		}
		if account.ID != req.Principal.UserID || !account.Active || account.AuthVersion != req.Principal.AuthVersion {
			return security.ErrUnauthenticated
		}
		if account.Username == "" || len(account.Username) > 256 {
			return security.ErrUnavailable
		}
		executionID, err := s.deps.IDs.GenerateID()
		if err != nil || !security.ValidID(executionID) {
			return security.ErrUnavailable
		}
		if err := s.authorize(locked, req, security.ActionExecuteMigration); err != nil {
			return err
		}
		claimed, err := s.deps.Repository.Reserve(locked, req, Claim{MigrationID: id, ExecutionID: executionID, Revision: revision, Checksum: Checksum(record.SQL), Actor: account.Username, At: s.deps.Clock.Now().UTC()})
		if err != nil {
			return safeError(err)
		}
		result = ExecutionResult{MigrationID: id, ExecutionID: executionID, Status: Pending}
		if !validRecord(claimed, req.DatabaseID) || claimed.ID != id || claimed.Status != Pending || claimed.ExecutionID != executionID || claimed.Checksum != Checksum(record.SQL) || claimed.Revision != revision+1 || claimed.ActorID != req.Principal.UserID || claimed.Actor != account.Username || claimed.Title != record.Title {
			return ErrExecutionUncertain
		}
		result = resultFor(claimed)
		// Setelah reservasi, kegagalan pencatatan atau commit harus tetap dianggap pending.
		if err := s.authorize(locked, req, security.ActionExecuteMigration); err != nil {
			finishErr := s.finish(locked, claimed, Failed, FailureInterrupted, &result)
			if finishErr != nil {
				return finishErr
			}
			return err
		}
		journaled := false
		if targetJournal, ok := target.(JournaledTarget); ok {
			if err := targetJournal.StartReceipt(locked, Receipt{Key: keyFor(claimed)}); err != nil {
				return s.finish(locked, claimed, Failed, FailureConnection, &result)
			}
			journaled = true
		}
		tx, err := target.Begin(locked)
		if err != nil || missing(tx) {
			return s.finish(locked, claimed, Failed, FailureConnection, &result)
		}
		closed := false
		defer func() {
			if !closed {
				cleanup, stop := cleanupContext(locked)
				defer stop()
				_ = tx.Rollback(cleanup)
			}
		}()
		if err := s.authorize(locked, req, security.ActionExecuteMigration); err != nil {
			cleanup, stop := cleanupContext(locked)
			defer stop()
			if tx.Rollback(cleanup) != nil {
				return ErrExecutionUncertain
			}
			closed = true
			if finishErr := s.finish(locked, claimed, Failed, FailureInterrupted, &result); finishErr != nil {
				return finishErr
			}
			return err
		}
		if err := tx.Execute(locked, claimed.SQL); err != nil {
			if journaled {
				return s.rollbackUncertain(locked, tx, &closed)
			}
			return s.rollback(locked, tx, claimed, &result, &closed)
		}
		if err := tx.WriteReceipt(locked, Receipt{Key: keyFor(claimed)}); err != nil {
			if journaled {
				return s.rollbackUncertain(locked, tx, &closed)
			}
			return s.rollback(locked, tx, claimed, &result, &closed)
		}
		if err := tx.Commit(locked); err != nil {
			return ErrExecutionUncertain
		}
		closed = true
		return s.finish(locked, claimed, Applied, FailureNone, &result)
	})
	if result.Status == Pending {
		return result, ErrExecutionUncertain
	}
	if err != nil {
		return result, safeValidationError(err)
	}
	if result.MigrationID == "" {
		return result, security.ErrUnavailable
	}
	return result, nil
}

func (s *Service) rollbackUncertain(ctx context.Context, tx Transaction, closed *bool) error {
	cleanup, stop := cleanupContext(ctx)
	defer stop()
	if err := tx.Rollback(cleanup); err == nil {
		*closed = true
	}
	return ErrExecutionUncertain
}

func (s *Service) finish(ctx context.Context, record Migration, status Status, failure Failure, result *ExecutionResult) error {
	cleanup, stop := cleanupContext(ctx)
	defer stop()
	if err := s.deps.Repository.Finish(cleanup, keyFor(record), status, failure, s.deps.Clock.Now().UTC()); err != nil {
		return ErrExecutionUncertain
	}
	record.Status, record.Failure = status, failure
	*result = resultFor(record)
	return nil
}

func (s *Service) rollback(ctx context.Context, tx Transaction, record Migration, result *ExecutionResult, closed *bool) error {
	cleanup, stop := cleanupContext(ctx)
	defer stop()
	if err := tx.Rollback(cleanup); err != nil {
		return ErrExecutionUncertain
	}
	*closed = true
	return s.finish(ctx, record, Failed, FailureSQL, result)
}

type ReconcilerConfig struct {
	Repository  Repository
	Coordinator Coordinator
	Clock       security.Clock
}
type Reconciler struct{ deps ReconcilerConfig }

func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	for _, dep := range []any{cfg.Repository, cfg.Coordinator, cfg.Clock} {
		if missing(dep) {
			return nil, security.ErrInvalidInput
		}
	}
	return &Reconciler{deps: cfg}, nil
}

// Hanya worker internal yang memanggil rekonsiliasi; metode ini tidak pernah menjalankan ulang SQL.
func (r *Reconciler) Reconcile(ctx context.Context, database, id security.ID) (ExecutionResult, error) {
	if !security.ValidID(database) || !security.ValidID(id) {
		return ExecutionResult{}, security.ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, ExecutionTimeout)
	defer cancel()
	var result ExecutionResult
	err := r.deps.Coordinator.WithDatabaseLock(ctx, database, func(locked context.Context, target LockedTarget) error {
		record, err := r.deps.Repository.Find(locked, database, id)
		if err != nil {
			return safeError(err)
		}
		if record.ID != id || !validRecord(record, database) || record.Status == DraftStatus {
			return security.ErrUnavailable
		}
		result = resultFor(record)
		if record.Status != Pending {
			return nil
		}
		if missing(target) {
			return ErrExecutionUncertain
		}
		lookup, err := target.LookupReceipt(locked, keyFor(record))
		if err != nil || !lookup.Quiescent {
			return ErrExecutionUncertain
		}
		if lookup.Started && lookup.Receipt == nil {
			return ErrExecutionUncertain
		}
		status, failure := Failed, FailureInterrupted
		if lookup.Receipt != nil {
			if lookup.Receipt.Key != keyFor(record) {
				return ErrExecutionUncertain
			}
			status, failure = Applied, FailureNone
		}
		cleanup, stop := cleanupContext(locked)
		defer stop()
		if err := r.deps.Repository.Finish(cleanup, keyFor(record), status, failure, r.deps.Clock.Now().UTC()); err != nil {
			return ErrExecutionUncertain
		}
		result.Status, result.Failure = status, failure
		return nil
	})
	if result.Status == Pending {
		return result, ErrExecutionUncertain
	}
	if result.MigrationID == "" && err == nil {
		return result, security.ErrUnavailable
	}
	return result, safeError(err)
}
