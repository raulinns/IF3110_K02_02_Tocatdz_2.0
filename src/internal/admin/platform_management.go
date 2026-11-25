package admin

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type GroupSummary struct {
	ID          security.ID
	Name        string
	LeaderID    security.ID // Empty for a group with no leader.
	MemberCount int
}

// M2Repository extends the M1 admin store. Every mutation rechecks the actor
// in the transaction and serializes with affected workflow and grant changes.
// Result records describe the committed mutation, including unchanged replays.
type M2Repository interface {
	// Group reads include only live groups and return public metadata.
	GroupSummaries(context.Context) ([]GroupSummary, error)
	GroupMembers(context.Context, security.ID) ([]User, error) // Missing group is ErrNotFound.
	// UpdateUser applies both fields atomically. Nil active and empty passwordHash
	// leave their respective fields unchanged. Password reset revokes sessions.
	// Reject deactivating the last active admin (ErrConflict).
	UpdateUser(ctx context.Context, actor security.Principal, userID security.ID, active *bool, passwordHash security.Secret) (User, error)
	RenameGroup(ctx context.Context, actor security.Principal, groupID security.ID, name string) (GroupSummary, error)
	// Deletes tombstone resources and reject conflicts with unresolved work.
	DeleteGroup(ctx context.Context, actor security.Principal, groupID security.ID) error
	DeleteConnection(ctx context.Context, actor security.Principal, databaseID security.ID) error
	// AddMemberByID returns false for an already existing membership.
	AddMemberByID(ctx context.Context, actor security.Principal, groupID, userID security.ID) (bool, error)
	// RemoveMember rejects removal of the current leader in the same transaction.
	// An absent membership returns ErrNotFound.
	RemoveMember(ctx context.Context, actor security.Principal, groupID, userID security.ID) error
	// SetLeader requires an active member and returns whether the leader changed.
	SetLeader(ctx context.Context, actor security.Principal, groupID, userID security.ID) (GroupSummary, bool, error)
	// PutGrant returns whether the effective group grant row changed.
	PutGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) (Grant, bool, error)
}

type M2Service struct {
	base *Service
	repo M2Repository
}

func NewM2Service(base *Service, repo M2Repository) (*M2Service, error) {
	if base == nil || missing(repo) {
		return nil, security.ErrInvalidInput
	}
	return &M2Service{base: base, repo: repo}, nil
}

func (s *M2Service) GroupSummaries(ctx context.Context, actor security.Principal) ([]GroupSummary, error) {
	if err := s.base.require(ctx, actor); err != nil {
		return nil, err
	}
	groups, err := s.repo.GroupSummaries(ctx)
	return groups, safeError(err)
}

func (s *M2Service) GroupMembers(ctx context.Context, actor security.Principal, groupID security.ID) ([]User, error) {
	if err := s.requireIDs(ctx, actor, groupID); err != nil {
		return nil, err
	}
	members, err := s.repo.GroupMembers(ctx, groupID)
	return members, safeError(err)
}

func (s *M2Service) UpdateUser(ctx context.Context, actor security.Principal, userID security.ID, active *bool, password *security.Secret) (User, error) {
	if err := s.requireIDs(ctx, actor, userID); err != nil {
		return User{}, err
	}
	if active == nil && password == nil {
		return User{}, security.ErrInvalidInput
	}
	if userID == actor.UserID && active != nil && !*active {
		return User{}, security.ErrConflict
	}
	var hash security.Secret
	if password != nil {
		if err := security.ValidateNewPassword(*password); err != nil {
			return User{}, safeError(err)
		}
		var err error
		hash, err = s.base.deps.Passwords.Hash(ctx, *password)
		if err != nil {
			return User{}, safeError(err)
		}
	}
	user, err := s.repo.UpdateUser(ctx, actor, userID, active, hash)
	return user, safeError(err)
}

func (s *M2Service) RenameGroup(ctx context.Context, actor security.Principal, groupID security.ID, name string) (GroupSummary, error) {
	if err := s.requireIDs(ctx, actor, groupID); err != nil {
		return GroupSummary{}, err
	}
	name, ok := validName(name)
	if !ok {
		return GroupSummary{}, security.ErrInvalidInput
	}
	group, err := s.repo.RenameGroup(ctx, actor, groupID, name)
	return group, safeError(err)
}

func (s *M2Service) DeleteGroup(ctx context.Context, actor security.Principal, groupID security.ID) error {
	if err := s.requireIDs(ctx, actor, groupID); err != nil {
		return err
	}
	return safeError(s.repo.DeleteGroup(ctx, actor, groupID))
}

func (s *M2Service) DeleteConnection(ctx context.Context, actor security.Principal, databaseID security.ID) error {
	if err := s.requireIDs(ctx, actor, databaseID); err != nil {
		return err
	}
	return safeError(s.repo.DeleteConnection(ctx, actor, databaseID))
}

func (s *M2Service) AddMemberByID(ctx context.Context, actor security.Principal, groupID, userID security.ID) (bool, error) {
	if err := s.requireIDs(ctx, actor, groupID, userID); err != nil {
		return false, err
	}
	changed, err := s.repo.AddMemberByID(ctx, actor, groupID, userID)
	return changed, safeError(err)
}

func (s *M2Service) RemoveMember(ctx context.Context, actor security.Principal, groupID, userID security.ID) (bool, error) {
	if err := s.requireIDs(ctx, actor, groupID, userID); err != nil {
		return false, err
	}
	err := s.repo.RemoveMember(ctx, actor, groupID, userID)
	return err == nil, safeError(err)
}

func (s *M2Service) SetLeader(ctx context.Context, actor security.Principal, groupID, userID security.ID) (GroupSummary, bool, error) {
	if err := s.requireIDs(ctx, actor, groupID, userID); err != nil {
		return GroupSummary{}, false, err
	}
	group, changed, err := s.repo.SetLeader(ctx, actor, groupID, userID)
	return group, changed, safeError(err)
}

func (s *M2Service) PutGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) (Grant, bool, error) {
	if err := s.requireIDs(ctx, actor, groupID, databaseID); err != nil {
		return Grant{}, false, err
	}
	if access != security.AccessViewOnly && access != security.AccessViewEdit {
		return Grant{}, false, security.ErrInvalidInput
	}
	grant, changed, err := s.repo.PutGrant(ctx, actor, groupID, databaseID, access)
	return grant, changed, safeError(err)
}

func (s *M2Service) requireIDs(ctx context.Context, actor security.Principal, ids ...security.ID) error {
	if err := s.base.require(ctx, actor); err != nil {
		return err
	}
	for _, id := range ids {
		if !security.ValidID(id) {
			return security.ErrInvalidInput
		}
	}
	return nil
}
