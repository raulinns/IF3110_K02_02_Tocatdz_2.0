package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var _ admin.M2Repository = (*AdminStore)(nil)

func (s *AdminStore) GroupSummaries(ctx context.Context) ([]admin.GroupSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.id::text, g.name, COALESCE(g.leader_user_id::text, ''), count(m.user_id)
		FROM groups g LEFT JOIN group_memberships m ON m.group_id = g.id GROUP BY g.id ORDER BY g.name, g.id`)
	groups, err := collect(rows, err, func(rows pgx.Rows) (admin.GroupSummary, error) {
		var group admin.GroupSummary
		var id, leader string
		if err := rows.Scan(&id, &group.Name, &leader, &group.MemberCount); err != nil {
			return group, mapError(err)
		}
		group.ID, group.LeaderID = security.ID(id), security.ID(leader)
		return group, nil
	})
	if groups == nil && err == nil {
		groups = []admin.GroupSummary{}
	}
	return groups, err
}

func groupSummary(ctx context.Context, q querier, groupID security.ID) (admin.GroupSummary, error) {
	group := admin.GroupSummary{ID: groupID}
	var leader string
	err := q.QueryRow(ctx, `SELECT g.name, COALESCE(g.leader_user_id::text, ''), (SELECT count(*) FROM group_memberships m WHERE m.group_id = g.id)
		FROM groups g WHERE g.id = $1`, string(groupID)).Scan(&group.Name, &leader, &group.MemberCount)
	group.LeaderID = security.ID(leader)
	return group, mapError(err)
}

func (s *AdminStore) GroupMembers(ctx context.Context, groupID security.ID) ([]admin.User, error) {
	if _, err := groupSummary(ctx, s.pool, groupID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id::text, u.username, u.active, u.platform_admin FROM group_memberships m
		JOIN users u ON u.id = m.user_id WHERE m.group_id = $1 ORDER BY u.username`, string(groupID))
	users, err := collect(rows, err, func(rows pgx.Rows) (admin.User, error) {
		var user admin.User
		var id string
		err := rows.Scan(&id, &user.Username, &user.Active, &user.PlatformAdmin)
		user.ID = security.ID(id)
		return user, mapError(err)
	})
	if users == nil && err == nil {
		users = []admin.User{}
	}
	return users, err
}

// Menonaktifkan akun atau reset password mencabut seluruh session akun tersebut.
func (s *AdminStore) UpdateUser(ctx context.Context, actor security.Principal, userID security.ID, active *bool, passwordHash security.Secret) (admin.User, error) {
	if active == nil && passwordHash.Empty() {
		return admin.User{}, security.ErrInvalidInput
	}
	var result admin.User
	err := s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		if err := lockGuards(ctx, tx, databasesOfUser, userID); err != nil {
			return noChange, nil, err
		}
		var id string
		if err := tx.QueryRow(ctx, "SELECT id::text, username, active, platform_admin FROM users WHERE id = $1 FOR UPDATE", string(userID)).
			Scan(&id, &result.Username, &result.Active, &result.PlatformAdmin); err != nil {
			return noChange, nil, mapError(err)
		}
		result.ID = security.ID(id)
		kind := noChange
		if active != nil && *active != result.Active {
			if !*active && result.PlatformAdmin {
				var others int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM users WHERE platform_admin AND active AND id <> $1", string(userID)).Scan(&others); err != nil {
					return noChange, nil, mapError(err)
				}
				if others == 0 {
					return noChange, nil, security.ErrConflict
				}
			}
			if _, err := tx.Exec(ctx, "UPDATE users SET active = $2 WHERE id = $1", string(userID), *active); err != nil {
				return noChange, nil, mapError(err)
			}
			result.Active = *active
			kind = elevation
			if !*active {
				kind = reduction
			}
		}
		if !passwordHash.Empty() {
			if _, err := tx.Exec(ctx, "UPDATE users SET password_hash = $2 WHERE id = $1", string(userID), passwordHash.Reveal()); err != nil {
				return noChange, nil, mapError(err)
			}
			kind = reduction
		}
		if kind == noChange {
			return noChange, nil, nil
		}
		return kind, []security.ID{userID}, nil
	})
	return result, err
}

func (s *AdminStore) RenameGroup(ctx context.Context, actor security.Principal, groupID security.ID, name string) (admin.GroupSummary, error) {
	var result admin.GroupSummary
	err := s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		tag, err := tx.Exec(ctx, "UPDATE groups SET name = $2 WHERE id = $1", string(groupID), name)
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if tag.RowsAffected() == 0 {
			return noChange, nil, security.ErrNotFound
		}
		result, err = groupSummary(ctx, tx, groupID)
		return noChange, nil, err
	})
	return result, err
}

const openChangeRequests = `status IN ('pending_review', 'approved', 'queued', 'running', 'uncertain')`

// Group yang memberi akses ke database dengan CR terbuka tidak boleh dihapus karena snapshot approval-nya masih dipakai.
func (s *AdminStore) DeleteGroup(ctx context.Context, actor security.Principal, groupID security.ID) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		if err := lockGuards(ctx, tx, databasesOfGroup, groupID); err != nil {
			return noChange, nil, err
		}
		var busy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM change_requests c JOIN group_database_grants x ON x.database_id = c.database_id
			WHERE x.group_id = $1 AND c.`+openChangeRequests+`)`, string(groupID)).Scan(&busy); err != nil {
			return noChange, nil, mapError(err)
		}
		if busy {
			return noChange, nil, security.ErrConflict
		}
		members, err := groupMembers(ctx, tx, groupID)
		if err != nil {
			return noChange, nil, err
		}
		tag, err := tx.Exec(ctx, "DELETE FROM groups WHERE id = $1", string(groupID))
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if tag.RowsAffected() == 0 {
			return noChange, nil, security.ErrNotFound
		}
		return reduction, members, nil
	})
}

// Database yang sudah punya riwayat workflow tidak boleh dihapus agar audit tetap utuh.
func (s *AdminStore) DeleteConnection(ctx context.Context, actor security.Principal, databaseID security.ID) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		if err := lockGuard(ctx, tx, databaseID); err != nil {
			return noChange, nil, err
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM change_requests WHERE database_id = $1)
			OR EXISTS (SELECT 1 FROM executions WHERE database_id = $1)
			OR EXISTS (SELECT 1 FROM migrations WHERE database_id = $1 AND (status <> 'draft' OR frozen_at IS NOT NULL))`, string(databaseID)).Scan(&used); err != nil {
			return noChange, nil, mapError(err)
		}
		if used {
			return noChange, nil, security.ErrConflict
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT m.user_id::text FROM group_database_grants x JOIN group_memberships m ON m.group_id = x.group_id
			WHERE x.database_id = $1`, string(databaseID))
		affected, err := collect(rows, err, func(rows pgx.Rows) (security.ID, error) {
			var id string
			err := rows.Scan(&id)
			return security.ID(id), mapError(err)
		})
		if err != nil {
			return noChange, nil, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM migrations WHERE database_id = $1", string(databaseID)); err != nil {
			return noChange, nil, mapError(err)
		}
		tag, err := tx.Exec(ctx, "DELETE FROM managed_databases WHERE id = $1", string(databaseID))
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if tag.RowsAffected() == 0 {
			return noChange, nil, security.ErrNotFound
		}
		return reduction, affected, nil
	})
}

func (s *AdminStore) AddMemberByID(ctx context.Context, actor security.Principal, groupID, userID security.ID) (bool, error) {
	changed := false
	err := s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		if err := lockGuards(ctx, tx, databasesOfGroup, groupID); err != nil {
			return noChange, nil, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1) AND EXISTS (SELECT 1 FROM users WHERE id = $2)",
			string(groupID), string(userID)).Scan(&exists); err != nil {
			return noChange, nil, mapError(err)
		}
		if !exists {
			return noChange, nil, security.ErrNotFound
		}
		tag, err := tx.Exec(ctx, "INSERT INTO group_memberships (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", string(groupID), string(userID))
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if changed = tag.RowsAffected() > 0; !changed {
			return noChange, nil, nil
		}
		return elevation, []security.ID{userID}, nil
	})
	return changed, err
}

// Leader wajib anggota aktif group.
func (s *AdminStore) SetLeader(ctx context.Context, actor security.Principal, groupID, userID security.ID) (admin.GroupSummary, bool, error) {
	var result admin.GroupSummary
	changed := false
	err := s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		if err := lockGuards(ctx, tx, databasesOfGroup, groupID); err != nil {
			return noChange, nil, err
		}
		current, err := groupSummary(ctx, tx, groupID)
		if err != nil {
			return noChange, nil, err
		}
		var eligible bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM group_memberships m JOIN users u ON u.id = m.user_id
			WHERE m.group_id = $1 AND m.user_id = $2 AND u.active)`, string(groupID), string(userID)).Scan(&eligible); err != nil {
			return noChange, nil, mapError(err)
		}
		if !eligible {
			return noChange, nil, security.ErrInvalidInput
		}
		if changed = current.LeaderID != userID; changed {
			if _, err := tx.Exec(ctx, "UPDATE groups SET leader_user_id = $2 WHERE id = $1", string(groupID), string(userID)); err != nil {
				return noChange, nil, mapError(err)
			}
		}
		result, err = groupSummary(ctx, tx, groupID)
		return noChange, nil, err
	})
	return result, changed, err
}

func (s *AdminStore) PutGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) (admin.Grant, bool, error) {
	result := admin.Grant{GroupID: groupID, DatabaseID: databaseID, Access: access}
	changed := false
	err := s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		if err := lockGuard(ctx, tx, databaseID); err != nil {
			return noChange, nil, err
		}
		if err := tx.QueryRow(ctx, "SELECT g.name, d.name FROM groups g, managed_databases d WHERE g.id = $1 AND d.id = $2",
			string(groupID), string(databaseID)).Scan(&result.GroupName, &result.DatabaseName); err != nil {
			return noChange, nil, mapError(err)
		}
		var previous *string
		err := tx.QueryRow(ctx, "SELECT access FROM group_database_grants WHERE group_id = $1 AND database_id = $2 FOR UPDATE",
			string(groupID), string(databaseID)).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return noChange, nil, mapError(err)
		}
		if previous != nil && security.DatabaseAccess(*previous) == access {
			return noChange, nil, nil
		}
		changed = true
		if _, err := tx.Exec(ctx, `INSERT INTO group_database_grants (group_id, database_id, access) VALUES ($1, $2, $3)
			ON CONFLICT (group_id, database_id) DO UPDATE SET access = EXCLUDED.access`, string(groupID), string(databaseID), string(access)); err != nil {
			return noChange, nil, mapError(err)
		}
		members, err := groupMembers(ctx, tx, groupID)
		if err != nil {
			return noChange, nil, err
		}
		if previous != nil && access == security.AccessViewOnly {
			return reduction, members, nil
		}
		return elevation, members, nil
	})
	return result, changed, err
}
