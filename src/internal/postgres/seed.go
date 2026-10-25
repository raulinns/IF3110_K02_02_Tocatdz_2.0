package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// SeedInput hanya untuk menyiapkan data demo lokal; nilai kosong berarti bagian itu dilewati.
type SeedInput struct {
	AdminUsername  string
	AdminHash      security.Secret
	ViewerUsername string
	ViewerHash     security.Secret
	TargetName     string
	TargetDSN      security.Secret
}

type idSource interface{ GenerateID() (security.ID, error) }

func upsertID(ctx context.Context, tx pgx.Tx, ids idSource, insert, lookup string, args ...any) (string, error) {
	id, err := ids.GenerateID()
	if err != nil {
		return "", security.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, insert, append([]any{string(id)}, args...)...); err != nil {
		return "", mapError(err)
	}
	var existing string
	if err := tx.QueryRow(ctx, lookup, args[0]).Scan(&existing); err != nil {
		return "", mapError(err)
	}
	return existing, nil
}

// Seed idempoten: menjalankannya berulang tidak menggandakan data dan tidak mengganti password yang sudah ada.
func Seed(ctx context.Context, pool *pgxpool.Pool, box *SecretBox, ids idSource, input SeedInput) error {
	if input.TargetName != "" && !input.TargetDSN.Empty() {
		dsn, err := PrepareManagedTarget(ctx, pool, input.TargetDSN, box)
		if err != nil {
			return err
		}
		input.TargetDSN = dsn
	}
	return withTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		adminID, err := upsertID(ctx, tx, ids, "INSERT INTO users (id, username, password_hash, platform_admin) VALUES ($1, $2, $3, true) ON CONFLICT (username) DO NOTHING",
			"SELECT id::text FROM users WHERE username = $1", input.AdminUsername, input.AdminHash.Reveal())
		if err != nil {
			return err
		}
		viewerID := ""
		if input.ViewerUsername != "" && !input.ViewerHash.Empty() {
			viewerID, err = upsertID(ctx, tx, ids, "INSERT INTO users (id, username, password_hash) VALUES ($1, $2, $3) ON CONFLICT (username) DO NOTHING",
				"SELECT id::text FROM users WHERE username = $1", input.ViewerUsername, input.ViewerHash.Reveal())
			if err != nil {
				return err
			}
		}
		if input.TargetName == "" || input.TargetDSN.Empty() {
			return nil
		}
		var databaseID string
		var previous []byte
		err = tx.QueryRow(ctx, "SELECT id::text,dsn_ciphertext FROM managed_databases WHERE name = $1 FOR UPDATE", input.TargetName).Scan(&databaseID, &previous)
		if errors.Is(err, pgx.ErrNoRows) {
			id, err := ids.GenerateID()
			if err != nil {
				return security.ErrUnavailable
			}
			sealed, err := box.Seal(id, input.TargetDSN)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, $2, $3)", string(id), input.TargetName, sealed); err != nil {
				return mapError(err)
			}
			databaseID = string(id)
		} else if err != nil {
			return mapError(err)
		}
		if len(previous) > 0 {
			old, err := box.Open(security.ID(databaseID), previous)
			if err != nil {
				return err
			}
			if old.Reveal() != input.TargetDSN.Reveal() {
				same, err := sameTargetDSN(ctx, old, input.TargetDSN)
				if err != nil || !same {
					return security.ErrConflict
				}
			}
		}
		sealed, err := box.Seal(security.ID(databaseID), input.TargetDSN)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE managed_databases SET dsn_ciphertext=$2 WHERE id=$1", databaseID, sealed); err != nil {
			return mapError(err)
		}
		groups := []struct {
			name, access, member string
		}{{"pengembang", "view_edit", adminID}, {"pengamat", "view_only", viewerID}}
		for _, group := range groups {
			groupID, err := upsertID(ctx, tx, ids, "INSERT INTO groups (id, name) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING",
				"SELECT id::text FROM groups WHERE name = $1", group.name)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO group_database_grants (group_id, database_id, access) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
				groupID, databaseID, group.access); err != nil {
				return mapError(err)
			}
			if group.member != "" {
				if _, err := tx.Exec(ctx, "INSERT INTO group_memberships (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", groupID, group.member); err != nil {
					return mapError(err)
				}
			}
		}
		return nil
	})
}
