package postgres

import (
	"context"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestSeedAssignsLeadersAndEditor(t *testing.T) {
	platform := testPlatform(t)
	ctx := context.Background()
	box, err := NewSecretBox(testKey(9))
	must(t, err)
	input := SeedInput{AdminUsername: "admin", AdminHash: security.NewSecret("hash-admin"), ViewerUsername: "pengamat",
		ViewerHash: security.NewSecret("hash-viewer"), EditorUsername: "editor", EditorHash: security.NewSecret("hash-editor"),
		TargetName: "demo", TargetDSN: security.NewSecret(createDatabase(t))}
	must(t, Seed(ctx, platform, box, security.NewTokenGenerator(), input))
	input.TargetName, input.TargetDSN = "gudang", security.NewSecret(createDatabase(t))
	must(t, Seed(ctx, platform, box, security.NewTokenGenerator(), input))

	rows, err := platform.Query(ctx, `SELECT g.name, l.username, count(DISTINCT m.user_id), count(DISTINCT x.database_id)
		FROM groups g JOIN users l ON l.id = g.leader_user_id JOIN group_memberships m ON m.group_id = g.id
		JOIN group_database_grants x ON x.group_id = g.id GROUP BY g.name, l.username ORDER BY g.name`)
	must(t, err)
	defer rows.Close()
	got := map[string][3]any{}
	for rows.Next() {
		var group, leader string
		var members, databases int
		must(t, rows.Scan(&group, &leader, &members, &databases))
		got[group] = [3]any{leader, members, databases}
	}
	if got["pengembang"] != [3]any{"admin", 2, 2} || got["pengamat"] != [3]any{"pengamat", 1, 2} {
		t.Fatalf("seeded groups = %v", got)
	}
}
