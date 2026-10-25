package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestPostgreSQLValidator(t *testing.T) {
	v := NewPostgreSQLValidator()
	for i, sql := range []string{
		`CREATE TABLE public.items (id integer PRIMARY KEY, name text NOT NULL); INSERT INTO public.items VALUES (1, 'BEGIN; COMMIT;');`,
		`ALTER TABLE public.items ADD COLUMN created_at timestamptz DEFAULT pg_catalog.now();`,
		`CREATE UNIQUE INDEX items_name_idx ON public.items (name);`,
		`ALTER TABLE public.items ADD CONSTRAINT items_check CHECK (id > 0);`,
		`UPDATE public.items SET name = 'x' WHERE id = 1; DELETE FROM public.items WHERE id = 2;`,
		`CREATE SCHEMA app; CREATE VIEW app.v AS SELECT 1 AS id;`,
		`DROP TABLE public.items;`,
		`WITH c AS (SELECT 1 AS id) INSERT INTO public.items (id) SELECT id FROM c;`,
		`-- COMMIT
CREATE TABLE public."a/b" (value text DEFAULT 'canary-secret');`,
	} {
		if err := v.Validate(context.Background(), security.NewSecret(sql)); err != nil {
			t.Errorf("accepted case %d rejected: %#v", i, err)
		}
	}
	for i, sql := range []string{
		`BEGIN; CREATE TABLE t(id int); COMMIT;`, `SAVEPOINT s;`, `ROLLBACK;`,
		`CREATE INDEX CONCURRENTLY idx ON t(id);`, `VACUUM t;`, `CREATE DATABASE x;`,
		`COPY t FROM PROGRAM 'cat /etc/passwd';`, `DO $$ BEGIN EXECUTE 'COMMIT'; END $$;`,
		`CALL public.run();`, `CREATE FUNCTION public.run() RETURNS int LANGUAGE sql AS 'SELECT 1';`,
		`SELECT pg_catalog.set_config('search_path','public',true);`, `SELECT public.dblink('canary-secret','SELECT 1');`,
		`ALTER TABLE t DISABLE TRIGGER ALL;`, `SET ROLE admin;`,
		`DROP SCHEMA tocatdz_internal CASCADE;`, `DELETE FROM tocatdz_internal.receipts;`,
		`SELECT * FROM pg_catalog.pg_authid;`, `CREATE TEMP TABLE t (id int);`,
		`CREATE SCHEMA pg_evil;`, `CREATE TABLE public.broken(`, `-- comment only`,
		`SELECT now();`, `SELECT 1 INTO new_table;`,
	} {
		err := v.Validate(context.Background(), security.NewSecret(sql))
		if !errors.Is(err, security.ErrInvalidInput) {
			t.Errorf("unsafe case %d not rejected: %v", i, err)
		}
		if err != nil && strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "canary-secret") {
			t.Fatal("parser error leaked SQL")
		}
	}
}

func TestDraftAndParserBoundaries(t *testing.T) {
	for _, draft := range []Draft{
		{Title: "", SQL: security.NewSecret("SELECT 1")},
		{Title: strings.Repeat("界", 121), SQL: security.NewSecret("SELECT 1")},
		{Title: "t", SQL: security.NewSecret(strings.Repeat("x", MaxSQLBytes+1))},
		{Title: "t", SQL: security.NewSecret("SELECT '\x00'")},
		{Title: "t", SQL: security.NewSecret(string([]byte{0xff}))},
	} {
		if !errors.Is(ValidateDraft(draft), security.ErrInvalidInput) {
			t.Fatal("invalid draft accepted")
		}
	}
	sql := security.NewSecret("  SELECT 1;  ")
	if ValidateDraft(Draft{Title: strings.Repeat("界", 120), SQL: sql}) != nil || sql.Reveal() != "  SELECT 1;  " {
		t.Fatal("valid draft changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if NewPostgreSQLValidator().Validate(ctx, sql) == nil {
		t.Fatal("cancelled parser accepted")
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", Draft{SQL: sql}, Draft{SQL: sql}, Draft{SQL: sql}), "SELECT 1") {
		t.Fatal("draft SQL leaked in formatting")
	}
}

func TestQualifyFunctionsOnlyTouchesUnqualifiedCalls(t *testing.T) {
	v := NewPostgreSQLValidator()
	sql := "-- now() di komentar\nALTER TABLE public.t ADD COLUMN a timestamptz DEFAULT now(), ADD COLUMN b text DEFAULT 'lower(x)', ADD COLUMN c text DEFAULT pg_catalog.upper('y'), ADD COLUMN d text DEFAULT public.fn(lower('z'));"
	got, err := v.QualifyFunctions(context.Background(), security.NewSecret(sql))
	want := "-- now() di komentar\nALTER TABLE public.t ADD COLUMN a timestamptz DEFAULT pg_catalog.now(), ADD COLUMN b text DEFAULT 'lower(x)', ADD COLUMN c text DEFAULT pg_catalog.upper('y'), ADD COLUMN d text DEFAULT public.fn(pg_catalog.lower('z'));"
	if err != nil || got.Reveal() != want {
		t.Fatalf("got %q, %v", got.Reveal(), err)
	}
	if _, err := v.QualifyFunctions(context.Background(), security.NewSecret("CREATE TABLE (")); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("syntax error not rejected")
	}
}
