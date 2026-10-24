package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestSeedUpgradesLegacyTargetWithoutResettingUsersOrHistory(t *testing.T) {
	ctx := context.Background()
	pool := testPlatform(t)
	box, _ := NewSecretBox(testKey(9))
	dsn := security.NewSecret(createDatabase(t))
	ids := security.NewTokenGenerator()
	input := SeedInput{AdminUsername: "admin", AdminHash: security.NewSecret("original-hash"), ViewerUsername: "pengamat", ViewerHash: security.NewSecret("viewer-hash"), TargetName: "demo", TargetDSN: dsn}
	if err := Seed(ctx, pool, box, ids, input); err != nil {
		t.Fatal(err)
	}
	var database string
	if err := pool.QueryRow(ctx, "SELECT id::text FROM managed_databases WHERE name='demo'").Scan(&database); err != nil {
		t.Fatal(err)
	}
	legacy, _ := box.Seal(security.ID(database), dsn)
	exec(t, pool, "UPDATE managed_databases SET dsn_ciphertext=$2 WHERE id=$1", database, legacy)
	input.AdminHash = security.NewSecret("replacement-hash")
	if err := Seed(ctx, pool, box, ids, input); err != nil {
		t.Fatal(err)
	}
	var hash string
	var users, groups, grants int
	var sealed []byte
	if err := pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE username='admin'").Scan(&hash); err != nil || hash != "original-hash" {
		t.Fatal("repeat seed reset existing password")
	}
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM groups),(SELECT count(*) FROM group_database_grants)").Scan(&users, &groups, &grants); err != nil || users != 2 || groups != 2 || grants != 2 {
		t.Fatal("seed duplicated data")
	}
	if err := pool.QueryRow(ctx, "SELECT dsn_ciphertext FROM managed_databases WHERE id=$1", database).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	runtime, err := box.Open(security.ID(database), sealed)
	if err != nil || runtime.Reveal() == dsn.Reveal() {
		t.Fatal("seed kept privileged legacy DSN")
	}
	targets, _ := NewTargets(pool, box)
	defer targets.Close()
	if err := targets.Check(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	input.TargetDSN = security.NewSecret(createDatabase(t))
	if err := Seed(ctx, pool, box, ids, input); !errors.Is(err, security.ErrConflict) {
		t.Fatal("seed replaced target of existing history")
	}
}
