package schemadiff

import (
	"strings"
	"testing"
)

func text(value string) *string { return &value }

func usersTable(columns ...Column) Table {
	return Table{Name: "users", Quoted: "users", Columns: columns}
}

func TestCompareIdenticalSchemasProducesNothing(t *testing.T) {
	snapshot := Snapshot{Schema: "public", Quoted: "public", Tables: []Table{usersTable(Column{Name: "id", Quoted: "id", Type: "integer", NotNull: true, Serial: true, Default: text("nextval('public.users_id_seq'::regclass)")})}}
	changes, warnings, sql := Compare(snapshot, snapshot)
	if len(changes) != 0 || len(warnings) != 0 || sql != "" {
		t.Fatalf("identical schema produced changes: %+v %v %q", changes, warnings, sql)
	}
}

func TestCompareOrdersStatementsSafely(t *testing.T) {
	current := Snapshot{Schema: "public", Quoted: "public",
		Tables: []Table{
			usersTable(Column{Name: "id", Quoted: "id", Type: "integer", NotNull: true}, Column{Name: "old", Quoted: "old", Type: "text"}),
			{Name: "audit", Quoted: "audit", Columns: []Column{{Name: "user_id", Quoted: "user_id", Type: "integer"}},
				Constraints: []Constraint{{Name: "audit_user_fkey", Quoted: "audit_user_fkey", Kind: "f", Definition: "FOREIGN KEY (user_id) REFERENCES public.users(id)"}}},
		},
		Views: []View{{Name: "v", Quoted: "v", Definition: "SELECT id FROM public.users"}},
	}
	desired := Snapshot{Schema: "public", Quoted: "public",
		Tables: []Table{
			usersTable(Column{Name: "id", Quoted: "id", Type: "bigint", NotNull: true}, Column{Name: "email", Quoted: "email", Type: "text", NotNull: true}),
			{Name: "orders", Quoted: "orders", Columns: []Column{{Name: "user_id", Quoted: "user_id", Type: "bigint"}},
				Constraints: []Constraint{{Name: "orders_user_fkey", Quoted: "orders_user_fkey", Kind: "f", Definition: "FOREIGN KEY (user_id) REFERENCES public.users(id)"}},
				Indexes:     []Index{{Name: "orders_user_idx", Quoted: "orders_user_idx", Tail: " USING btree (user_id)"}}},
		},
		Views: []View{{Name: "v", Quoted: "v", Definition: "SELECT id, email FROM public.users"}},
	}
	changes, warnings, sql := Compare(current, desired)
	order := []string{
		"DROP VIEW public.v;",
		"ALTER TABLE public.audit DROP CONSTRAINT audit_user_fkey;",
		"ALTER TABLE public.users ALTER COLUMN id TYPE bigint;",
		"ALTER TABLE public.users ADD COLUMN email text NOT NULL;",
		"ALTER TABLE public.users DROP COLUMN old;",
		"DROP TABLE public.audit;",
		"CREATE TABLE public.orders (",
		"ALTER TABLE public.orders ADD CONSTRAINT orders_user_fkey FOREIGN KEY",
		"CREATE INDEX orders_user_idx ON public.orders USING btree (user_id);",
		"CREATE VIEW public.v AS\nSELECT id, email FROM public.users;",
	}
	last := -1
	for _, statement := range order {
		at := strings.Index(sql, statement)
		if at < 0 || at < last {
			t.Fatalf("statement %q missing or out of order:\n%s", statement, sql)
		}
		last = at
	}
	destructive := 0
	for _, change := range changes {
		if change.Destructive {
			destructive++
		}
	}
	if destructive != 2 || len(warnings) < 2 {
		t.Fatalf("destructive changes or warnings not reported: %+v %v", changes, warnings)
	}
}

func TestCompareColumnDetails(t *testing.T) {
	current := Snapshot{Quoted: "public", Tables: []Table{usersTable(
		Column{Name: "note", Quoted: "note", Type: "text", Default: text("'a'::text")},
		Column{Name: "flag", Quoted: "flag", Type: "boolean", NotNull: true},
		Column{Name: "id", Quoted: "id", Type: "integer"},
	)}}
	desired := Snapshot{Quoted: "public", Tables: []Table{usersTable(
		Column{Name: "note", Quoted: "note", Type: "text", Default: text("'b'::text")},
		Column{Name: "flag", Quoted: "flag", Type: "boolean"},
		Column{Name: "id", Quoted: "id", Type: "integer", Identity: "d", NotNull: true},
	)}}
	_, warnings, sql := Compare(current, desired)
	for _, want := range []string{"ALTER COLUMN note SET DEFAULT 'b'::text;", "ALTER COLUMN flag DROP NOT NULL;", "ALTER COLUMN id SET NOT NULL;"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "IDENTITY") || len(warnings) != 2 {
		t.Fatalf("identity change must be left to the user: %v\n%s", warnings, sql)
	}
	created := columnDefinition(Column{Quoted: `"Total Harga"`, Type: "numeric", Generated: "s", Default: text("(price * 2)")})
	if created != `"Total Harga" numeric GENERATED ALWAYS AS ((price * 2)) STORED` {
		t.Fatalf("generated column rendered wrong: %s", created)
	}
	if columnDefinition(Column{Quoted: "id", Type: "bigint", Serial: true, NotNull: true, Default: text("nextval('x'::regclass)")}) != "id bigserial NOT NULL" {
		t.Fatal("serial column rendered wrong")
	}
}

func TestCompareRemapsOtherSchemaReferences(t *testing.T) {
	current := Snapshot{Schema: "public", Quoted: "public"}
	desired := Snapshot{Schema: "Toko Saya", Quoted: `"Toko Saya"`, Tables: []Table{{Name: "stok", Quoted: "stok",
		Columns:     []Column{{Name: "produk_id", Quoted: "produk_id", Type: "integer"}},
		Constraints: []Constraint{{Name: "stok_fkey", Quoted: "stok_fkey", Kind: "f", Definition: `FOREIGN KEY (produk_id) REFERENCES "Toko Saya".produk(id)`}},
	}}}
	_, _, sql := Compare(current, desired)
	if strings.Contains(sql, "Toko Saya") || !strings.Contains(sql, "REFERENCES public.produk(id)") || !strings.Contains(sql, "CREATE TABLE public.stok") {
		t.Fatalf("references not remapped:\n%s", sql)
	}
}
