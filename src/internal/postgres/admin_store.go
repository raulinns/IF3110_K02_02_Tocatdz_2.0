package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type AdminStore struct {
	pool  *pgxpool.Pool
	box   *SecretBox
	clock security.Clock
}

var _ admin.Repository = (*AdminStore)(nil)

func NewAdminStore(pool *pgxpool.Pool, box *SecretBox, clock security.Clock) (*AdminStore, error) {
	if pool == nil || box == nil || clock == nil {
		return nil, security.ErrInvalidInput
	}
	return &AdminStore{pool: pool, box: box, clock: clock}, nil
}

func (s *AdminStore) Users(ctx context.Context) ([]admin.User, error) {
	rows, err := s.pool.Query(ctx, "SELECT id::text, username, active, platform_admin FROM users ORDER BY username")
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []admin.User
	for rows.Next() {
		var user admin.User
		var id string
		if err := rows.Scan(&id, &user.Username, &user.Active, &user.PlatformAdmin); err != nil {
			return nil, mapError(err)
		}
		user.ID = security.ID(id)
		result = append(result, user)
	}
	return result, mapError(rows.Err())
}

func (s *AdminStore) Groups(ctx context.Context) ([]admin.Group, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.id::text, g.name, u.id::text, u.username, u.active, u.platform_admin FROM groups g
		LEFT JOIN group_memberships m ON m.group_id = g.id LEFT JOIN users u ON u.id = m.user_id ORDER BY g.name, u.username`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []admin.Group
	index := map[string]int{}
	for rows.Next() {
		var groupID, name string
		var userID, username *string
		var active, platformAdmin *bool
		if err := rows.Scan(&groupID, &name, &userID, &username, &active, &platformAdmin); err != nil {
			return nil, mapError(err)
		}
		position, ok := index[groupID]
		if !ok {
			position = len(result)
			index[groupID] = position
			result = append(result, admin.Group{ID: security.ID(groupID), Name: name})
		}
		if userID != nil {
			result[position].Members = append(result[position].Members, admin.User{ID: security.ID(*userID), Username: *username, Active: *active, PlatformAdmin: *platformAdmin})
		}
	}
	return result, mapError(rows.Err())
}

func (s *AdminStore) Connections(ctx context.Context) ([]admin.Connection, error) {
	rows, err := s.pool.Query(ctx, "SELECT id::text, name FROM managed_databases ORDER BY name")
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []admin.Connection
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, mapError(err)
		}
		result = append(result, admin.Connection{ID: security.ID(id), Name: name})
	}
	return result, mapError(rows.Err())
}

func (s *AdminStore) Grants(ctx context.Context) ([]admin.Grant, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.id::text, g.name, d.id::text, d.name, x.access FROM group_database_grants x
		JOIN groups g ON g.id = x.group_id JOIN managed_databases d ON d.id = x.database_id ORDER BY g.name, d.name`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []admin.Grant
	for rows.Next() {
		var grant admin.Grant
		var groupID, databaseID, access string
		if err := rows.Scan(&groupID, &grant.GroupName, &databaseID, &grant.DatabaseName, &access); err != nil {
			return nil, mapError(err)
		}
		grant.GroupID, grant.DatabaseID, grant.Access = security.ID(groupID), security.ID(databaseID), security.DatabaseAccess(access)
		result = append(result, grant)
	}
	return result, mapError(rows.Err())
}

type change int

const (
	noChange change = iota
	elevation
	reduction
)

// mutate menjalankan satu perubahan admin secara serial dan atomik bersama efek keamanannya.
func (s *AdminStore) mutate(ctx context.Context, actor security.Principal, run func(pgx.Tx) (change, []security.ID, error)) error {
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(adminLockKey)); err != nil {
			return mapError(err)
		}
		var active, platformAdmin bool
		var version int64
		err := tx.QueryRow(ctx, "SELECT active, platform_admin, auth_version FROM users WHERE id = $1 FOR SHARE", string(actor.UserID)).Scan(&active, &platformAdmin, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return security.ErrUnauthenticated
		}
		if err != nil {
			return mapError(err)
		}
		if !active || uint64(version) != actor.AuthVersion {
			return security.ErrUnauthenticated
		}
		if !platformAdmin {
			return security.ErrForbidden
		}
		kind, affected, err := run(tx)
		if err != nil || kind == noChange || len(affected) == 0 {
			return err
		}
		if _, err := lockAccounts(ctx, tx, affected...); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE users SET auth_version = auth_version + 1 WHERE id = ANY($1::uuid[])", ids(affected)); err != nil {
			return mapError(err)
		}
		// Pengurangan hak mencabut semua session terdampak; penambahan cukup lewat epoch sehingga session dirotasi saat validasi berikutnya.
		if kind == reduction {
			if _, err := tx.Exec(ctx, "UPDATE sessions SET revoked_at = $2, revision = revision + 1 WHERE user_id = ANY($1::uuid[]) AND revoked_at IS NULL",
				ids(affected), micro(s.clock.Now())); err != nil {
				return mapError(err)
			}
		}
		return nil
	})
}

func groupMembers(ctx context.Context, tx pgx.Tx, groupID security.ID) ([]security.ID, error) {
	rows, err := tx.Query(ctx, "SELECT user_id::text FROM group_memberships WHERE group_id = $1", string(groupID))
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []security.ID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, mapError(err)
		}
		result = append(result, security.ID(id))
	}
	return result, mapError(rows.Err())
}

func (s *AdminStore) CreateUser(ctx context.Context, actor security.Principal, user admin.User, hash security.Secret) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		_, err := tx.Exec(ctx, "INSERT INTO users (id, username, password_hash, active, platform_admin) VALUES ($1, $2, $3, true, $4)",
			string(user.ID), user.Username, hash.Reveal(), user.PlatformAdmin)
		return noChange, nil, mapError(err)
	})
}

func (s *AdminStore) SetUserActive(ctx context.Context, actor security.Principal, userID security.ID, active bool) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		tag, err := tx.Exec(ctx, "UPDATE users SET active = $2 WHERE id = $1 AND active <> $2", string(userID), active)
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)", string(userID)).Scan(&exists); err != nil {
				return noChange, nil, mapError(err)
			}
			if !exists {
				return noChange, nil, security.ErrNotFound
			}
			return noChange, nil, nil
		}
		if active {
			return elevation, []security.ID{userID}, nil
		}
		return reduction, []security.ID{userID}, nil
	})
}

func (s *AdminStore) CreateGroup(ctx context.Context, actor security.Principal, group admin.Group) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		_, err := tx.Exec(ctx, "INSERT INTO groups (id, name) VALUES ($1, $2)", string(group.ID), group.Name)
		return noChange, nil, mapError(err)
	})
}

func (s *AdminStore) AddMember(ctx context.Context, actor security.Principal, groupID security.ID, username string) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		var userID string
		if err := tx.QueryRow(ctx, "SELECT id::text FROM users WHERE username = $1", username).Scan(&userID); err != nil {
			return noChange, nil, mapError(err)
		}
		tag, err := tx.Exec(ctx, "INSERT INTO group_memberships (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", string(groupID), userID)
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if tag.RowsAffected() == 0 {
			return noChange, nil, nil
		}
		return elevation, []security.ID{security.ID(userID)}, nil
	})
}

func (s *AdminStore) RemoveMember(ctx context.Context, actor security.Principal, groupID, userID security.ID) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		tag, err := tx.Exec(ctx, "DELETE FROM group_memberships WHERE group_id = $1 AND user_id = $2", string(groupID), string(userID))
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if tag.RowsAffected() == 0 {
			return noChange, nil, security.ErrNotFound
		}
		return reduction, []security.ID{userID}, nil
	})
}

func (s *AdminStore) CreateConnection(ctx context.Context, actor security.Principal, connection admin.Connection, dsn security.Secret) error {
	sealed, err := s.box.Seal(connection.ID, dsn)
	if err != nil {
		return err
	}
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		_, err := tx.Exec(ctx, "INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, $2, $3)", string(connection.ID), connection.Name, sealed)
		return noChange, nil, mapError(err)
	})
}

func (s *AdminStore) SetGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID, access security.DatabaseAccess) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		var previous *string
		err := tx.QueryRow(ctx, "SELECT access FROM group_database_grants WHERE group_id = $1 AND database_id = $2 FOR UPDATE",
			string(groupID), string(databaseID)).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return noChange, nil, mapError(err)
		}
		if previous != nil && security.DatabaseAccess(*previous) == access {
			return noChange, nil, nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO group_database_grants (group_id, database_id, access) VALUES ($1, $2, $3)
			ON CONFLICT (group_id, database_id) DO UPDATE SET access = EXCLUDED.access`, string(groupID), string(databaseID), string(access)); err != nil {
			return noChange, nil, mapError(err)
		}
		members, err := groupMembers(ctx, tx, groupID)
		if err != nil {
			return noChange, nil, err
		}
		// Semua anggota ikut terdampak walaupun akses efektif mereka mungkin tidak berubah (kebijakan konservatif kontrak §5).
		if previous != nil && access == security.AccessViewOnly {
			return reduction, members, nil
		}
		return elevation, members, nil
	})
}

func (s *AdminStore) RevokeGrant(ctx context.Context, actor security.Principal, groupID, databaseID security.ID) error {
	return s.mutate(ctx, actor, func(tx pgx.Tx) (change, []security.ID, error) {
		tag, err := tx.Exec(ctx, "DELETE FROM group_database_grants WHERE group_id = $1 AND database_id = $2", string(groupID), string(databaseID))
		if err != nil {
			return noChange, nil, mapError(err)
		}
		if tag.RowsAffected() == 0 {
			return noChange, nil, security.ErrNotFound
		}
		members, err := groupMembers(ctx, tx, groupID)
		return reduction, members, err
	})
}
