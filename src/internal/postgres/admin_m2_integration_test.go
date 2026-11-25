package postgres

import (
	"context"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/admin"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestM2AdminRepository(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	must(t, f.store.AddMember(ctx, f.actor, f.group, "siti"))

	groups, err := f.store.GroupSummaries(ctx)
	must(t, err)
	if len(groups) != 1 || groups[0].MemberCount != 1 || groups[0].LeaderID != "" {
		t.Fatalf("groups = %+v", groups)
	}
	renamed, err := f.store.RenameGroup(ctx, f.actor, f.group, "tim-inti")
	must(t, err)
	if renamed.Name != "tim-inti" {
		t.Fatalf("renamed = %+v", renamed)
	}
	group, changed, err := f.store.SetLeader(ctx, f.actor, f.group, f.member)
	must(t, err)
	if !changed || group.LeaderID != f.member {
		t.Fatalf("leader = %+v changed=%v", group, changed)
	}
	if _, changed, err = f.store.SetLeader(ctx, f.actor, f.group, f.member); err != nil || changed {
		t.Fatalf("same leader again: changed=%v err=%v", changed, err)
	}
	members, err := f.store.GroupMembers(ctx, f.group)
	must(t, err)
	if len(members) != 1 || members[0].ID != f.member {
		t.Fatalf("members = %+v", members)
	}
	if added, err := f.store.AddMemberByID(ctx, f.actor, f.group, f.member); err != nil || added {
		t.Fatalf("existing member added again: %v %v", added, err)
	}
	grant, changed, err := f.store.PutGrant(ctx, f.actor, f.group, f.db, security.AccessViewEdit)
	must(t, err)
	if !changed || grant.DatabaseName != "toko" || grant.GroupName != "tim-inti" {
		t.Fatalf("grant = %+v", grant)
	}
	if _, changed, err = f.store.PutGrant(ctx, f.actor, f.group, f.db, security.AccessViewEdit); err != nil || changed {
		t.Fatalf("same grant again: changed=%v err=%v", changed, err)
	}

	// Admin aktif terakhir tidak bisa dinonaktifkan; reset password mencabut session.
	inactive := false
	_, err = f.store.UpdateUser(ctx, f.actor, f.actor.UserID, &inactive, security.Secret{})
	expectErr(t, err, security.ErrConflict)
	session := liveSession(t, f.pool, f.member)
	user, err := f.store.UpdateUser(ctx, f.actor, f.member, nil, security.NewSecret("hash-baru"))
	must(t, err)
	if user.ID != f.member || !revoked(t, f.pool, session) {
		t.Fatalf("password reset did not revoke session: %+v", user)
	}

	// Database dengan riwayat workflow tidak boleh dihapus; group dengan CR terbuka juga tidak.
	migration := newID(t)
	exec(t, f.pool, `INSERT INTO migrations (id, database_id, title, sql, status, revision, created_by, created_at, updated_at,
		down_sql, irreversible, irreversible_reason, definition_checksum, frozen_at)
		VALUES ($1, $2, 'm', 'SELECT 1', 'draft', 1, $3, now(), now(), 'SELECT 2', false, '', '`+checksumHex+`', now())`,
		string(migration), string(f.db), string(f.member))
	exec(t, f.pool, `INSERT INTO change_requests (id, database_id, migration_id, kind, status, migration_revision, checksum,
		expected_position_version, requested_by, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, 'apply', 'pending_review', 1, '`+checksumHex+`', 0, $3, now(), now())`,
		string(f.db), string(migration), string(f.member))
	expectErr(t, f.store.DeleteGroup(ctx, f.actor, f.group), security.ErrConflict)
	expectErr(t, f.store.DeleteConnection(ctx, f.actor, f.db), security.ErrConflict)

	other := newID(t)
	must(t, f.store.CreateGroup(ctx, f.actor, admin.Group{ID: other, Name: "sementara"}))
	must(t, f.store.DeleteGroup(ctx, f.actor, other))
	expectErr(t, f.store.DeleteGroup(ctx, f.actor, other), security.ErrNotFound)
}
