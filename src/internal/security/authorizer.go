package security

import (
	"context"
	"errors"
)

type BackendAuthorizer struct {
	sessions SessionRepository
	access   AccessRepository
	clock    Clock
	policy   SessionPolicy
}

var _ Authorizer = (*BackendAuthorizer)(nil)

func NewBackendAuthorizer(sessions SessionRepository, access AccessRepository, clock Clock, policy SessionPolicy) (*BackendAuthorizer, error) {
	if missingDependency(sessions) || missingDependency(access) || missingDependency(clock) || policy.Validate() != nil {
		return nil, ErrInvalidInput
	}
	return &BackendAuthorizer{sessions: sessions, access: access, clock: clock, policy: policy}, nil
}

func (a *BackendAuthorizer) current(ctx context.Context, principal Principal) (Account, error) {
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	if !ValidID(principal.UserID) || !ValidID(principal.SessionID) || principal.AuthVersion == 0 || principal.CSRFEpoch == 0 {
		return Account{}, ErrUnauthenticated
	}
	snapshot, err := a.sessions.FindByID(ctx, principal.SessionID)
	if errors.Is(err, ErrNotFound) {
		return Account{}, ErrUnauthenticated
	}
	if err != nil {
		return Account{}, backendError(err)
	}
	if !validSession(snapshot, a.clock.Now().UTC(), a.policy) || principalFor(snapshot.Session) != principal ||
		snapshot.Account.AuthVersion != principal.AuthVersion {
		return Account{}, ErrUnauthenticated
	}
	return snapshot.Account, nil
}

func (a *BackendAuthorizer) RequirePlatformAdmin(ctx context.Context, principal Principal) error {
	account, err := a.current(ctx, principal)
	if err != nil {
		return err
	}
	if !account.PlatformAdmin {
		return ErrForbidden
	}
	return nil
}

func (a *BackendAuthorizer) RequireDatabase(ctx context.Context, principal Principal, databaseID ID, action DatabaseAction) error {
	if !ValidID(databaseID) || !Allows(AccessViewEdit, action) {
		return ErrForbidden
	}
	if _, err := a.current(ctx, principal); err != nil {
		return err
	}
	snapshot, err := a.access.AccessForDatabase(ctx, principal.UserID, databaseID)
	if errors.Is(err, ErrNotFound) {
		return ErrForbidden
	}
	if err != nil {
		return backendError(err)
	}
	if snapshot.Account.ID != principal.UserID || !snapshot.Account.Active || snapshot.Account.AuthVersion != principal.AuthVersion {
		return ErrUnauthenticated
	}
	access, err := EffectiveAccess(snapshot.GroupAccess)
	if err != nil {
		return ErrUnavailable
	}
	if !Allows(access, action) {
		return ErrForbidden
	}
	// Cek session lagi setelah membaca grant supaya pencabutan akses yang terjadi di antaranya tidak terlewat.
	_, err = a.current(ctx, principal)
	return err
}
