package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const targetFixture = `
CREATE TABLE public.users (id integer PRIMARY KEY, email text NOT NULL UNIQUE, avatar bytea, note text DEFAULT 'baru' CHECK (note <> 'x'));
CREATE TABLE public.audit (message text);
CREATE VIEW public.active_users AS SELECT id, email FROM public.users;
CREATE SCHEMA "Toko Saya";
CREATE TABLE "Toko Saya"."a/b" (id integer);
INSERT INTO public.users (id, email, avatar, note) VALUES (1, 'a@x.test', '\x00ff', NULL), (2, 'b@x.test', NULL, repeat('é', 3000)), (3, 'c@x.test', NULL, 'ok');
`

type targetFixtureEnv struct {
	platform *pgxpool.Pool
	target   *pgxpool.Pool
	targets  *Targets
	database security.ID
}

func newTargetFixture(t *testing.T) *targetFixtureEnv {
	t.Helper()
	platform := testPlatform(t)
	dsn := createDatabase(t)
	target, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.Close)
	exec(t, target, targetFixture)
	box, err := NewSecretBox(testKey(9))
	if err != nil {
		t.Fatal(err)
	}
	database := newID(t)
	runtimeDSN, err := PrepareManagedTarget(context.Background(), platform, security.NewSecret(dsn), box)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal(database, runtimeDSN)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, platform, "INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, 'toko', $2)", string(database), sealed)
	targets, err := NewTargets(platform, box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(targets.Close)
	return &targetFixtureEnv{platform: platform, target: target, targets: targets, database: database}
}

func TestSecretBoxBindsCiphertextToDatabase(t *testing.T) {
	box, _ := NewSecretBox(testKey(9))
	id, other := security.ID("3f1c2d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"), security.ID("6a7b8c9d-0e1f-4a2b-9c3d-4e5f6a7b8c9d")
	sealed, err := box.Seal(id, security.NewSecret("postgres://u:canary-secret@h/db"))
	if err != nil || strings.Contains(string(sealed), "canary-secret") {
		t.Fatal("DSN not encrypted")
	}
	if plain, err := box.Open(id, sealed); err != nil || plain.Reveal() != "postgres://u:canary-secret@h/db" {
		t.Fatal("round trip failed")
	}
	if _, err := box.Open(other, sealed); err == nil {
		t.Fatal("ciphertext opened for another database id")
	}
	if wrong, _ := NewSecretBox(testKey(8)); wrong != nil {
		if _, err := wrong.Open(id, sealed); err == nil {
			t.Fatal("ciphertext opened with another key")
		}
	}
	if _, err := NewSecretBox(security.NewSecret("pendek")); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestTargetCatalogReads(t *testing.T) {
	env := newTargetFixture(t)
	ctx := context.Background()
	schemas, err := env.targets.Schemas(ctx, env.database)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string][]explorer.TableSummary{}
	for _, schema := range schemas {
		names[schema.Name] = schema.Tables
		if strings.HasPrefix(schema.Name, "pg_") || schema.Name == "information_schema" {
			t.Fatal("system schema exposed")
		}
	}
	if len(names["public"]) != 3 || len(names["Toko Saya"]) != 1 || names["Toko Saya"][0].Name != "a/b" {
		t.Fatalf("unexpected catalog: %+v", names)
	}
	table, err := env.targets.Table(ctx, env.database, "public", "users")
	if err != nil || table.Kind != explorer.KindTable || len(table.Columns) != 4 {
		t.Fatal("table detail incomplete")
	}
	kinds := map[string]bool{}
	for _, constraint := range table.Constraints {
		kinds[constraint.Type] = true
	}
	if !kinds["PRIMARY KEY"] || !kinds["UNIQUE"] || !kinds["CHECK"] {
		t.Fatalf("constraints missing: %+v", table.Constraints)
	}
	primary := false
	for _, index := range table.Indexes {
		primary = primary || index.Primary
	}
	if !primary || table.Columns[3].Default == nil || !strings.Contains(*table.Columns[3].Default, "baru") {
		t.Fatal("index or default missing")
	}
	if _, err := env.targets.Table(ctx, env.database, "public", "missing"); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("missing table not reported as not found")
	}
	if _, err := env.targets.Table(ctx, env.database, "pg_catalog", "pg_authid"); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("system relation readable")
	}
}

func TestTargetRowsArePagedOrderedAndBounded(t *testing.T) {
	env := newTargetFixture(t)
	ctx := context.Background()
	options := explorer.RowReadOptions{Offset: 0, FetchLimit: 2, MaxCellBytes: explorer.MaxCellBytes, MaxPayloadBytes: explorer.MaxPayloadBytes}
	batch, err := env.targets.Rows(ctx, env.database, "public", "users", options)
	if err != nil || batch.Order != explorer.OrderPrimaryKey || len(batch.Rows) != 2 || len(batch.Columns) != 4 {
		t.Fatalf("first page wrong: %+v %v", batch, err)
	}
	if batch.Rows[0][0].Text != "1" || batch.Rows[0][2].Encoding != explorer.EncodingHex || batch.Rows[0][2].Text != `\x00ff` || !batch.Rows[0][3].Null {
		t.Fatalf("cell conversion wrong: %+v", batch.Rows[0])
	}
	if !batch.Rows[1][3].Truncated || len(batch.Rows[1][3].Text) > explorer.MaxCellBytes {
		t.Fatal("long text not truncated")
	}
	options.Offset = 2
	batch, err = env.targets.Rows(ctx, env.database, "public", "users", options)
	if err != nil || len(batch.Rows) != 1 || batch.Rows[0][0].Text != "3" {
		t.Fatal("second page wrong")
	}
	batch, err = env.targets.Rows(ctx, env.database, "public", "audit", options)
	if err != nil || batch.Order != explorer.OrderBestEffort {
		t.Fatal("table without primary key not marked best effort")
	}
	if _, err := env.targets.Rows(ctx, env.database, "Toko Saya", "a/b", options); err != nil {
		t.Fatal("quoted identifiers not handled")
	}
	if _, err := env.targets.Rows(ctx, env.database, "public", `users"; DROP TABLE public.users; --`, options); !errors.Is(err, security.ErrNotFound) {
		t.Fatal("injected name not rejected via catalog lookup")
	}
	var count int
	if err := env.target.QueryRow(ctx, "SELECT count(*) FROM public.users").Scan(&count); err != nil || count != 3 {
		t.Fatal("explorer modified target data")
	}
}

func TestTargetReadsAreReadOnly(t *testing.T) {
	env := newTargetFixture(t)
	ctx := context.Background()
	err := env.targets.read(ctx, env.database, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM public.users")
		return err
	})
	var count int
	if err == nil || env.target.QueryRow(ctx, "SELECT count(*) FROM public.users").Scan(&count) != nil || count != 3 {
		t.Fatal("explorer read transaction allowed a write")
	}
}

func TestUnreachableTargetIsUnavailable(t *testing.T) {
	platform := testPlatform(t)
	box, _ := NewSecretBox(testKey(9))
	database := newID(t)
	sealed, _ := box.Seal(database, security.NewSecret("postgres://nobody:canary-secret@127.0.0.1:1/none?connect_timeout=1"))
	exec(t, platform, "INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, 'mati', $2)", string(database), sealed)
	targets, _ := NewTargets(platform, box)
	defer targets.Close()
	if _, err := targets.Schemas(context.Background(), database); !errors.Is(err, security.ErrUnavailable) || strings.Contains(err.Error(), "canary") {
		t.Fatalf("connection failure not mapped safely: %v", err)
	}
	if err := targets.Check(context.Background(), security.NewSecret("postgres://nobody@127.0.0.1:1/none?connect_timeout=1")); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("unreachable DSN accepted by check")
	}
	var _ migration.Coordinator = targets
}
