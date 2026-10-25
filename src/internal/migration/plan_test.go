package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestPlanStatementUsesParserBoundaries(t *testing.T) {
	v := NewPostgreSQLValidator()
	ctx := context.Background()
	sql := security.NewSecret("-- awal é\nSELECT 'é; bukan pemisah'; /* ; */ SELECT 2; -- akhir")
	for index, want := range map[int]string{1: "SELECT 'é; bukan pemisah'", 2: "SELECT 2"} {
		query, err := v.PlanStatement(ctx, sql, index, true)
		if err != nil || !strings.Contains(query.Reveal(), want) || index == 2 && strings.Contains(query.Reveal(), "SELECT 'é") {
			t.Fatalf("wrong statement %d: %v", index, err)
		}
	}
	for _, test := range []struct {
		sql     string
		index   int
		analyze bool
		reason  PlanRejection
	}{
		{"CREATE TABLE public.x(id int); SELECT 1;", 1, false, "statement"},
		{"SELECT 1;", 2, false, "statement"},
		{"UPDATE public.x SET id=1", 1, true, "analyze"},
		{"WITH changed AS (DELETE FROM public.x RETURNING *) SELECT * FROM changed", 1, true, "analyze"},
	} {
		_, err := v.PlanStatement(ctx, security.NewSecret(test.sql), test.index, test.analyze)
		var reason PlanRejection
		if !errors.As(err, &reason) || reason != test.reason {
			t.Fatalf("expected %s, got %v", test.reason, err)
		}
	}
	if _, err := v.PlanStatement(ctx, security.NewSecret("UPDATE public.x SET id=1;"), 1, false); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"SELECT 1; DROP DATABASE postgres;", "SELECT pg_catalog.nextval('s');", "SELECT 1; SELECT * FROM tocatdz_internal.receipt_keys;"} {
		if _, err := v.PlanStatement(ctx, security.NewSecret(sql), 1, false); !errors.Is(err, security.ErrInvalidInput) {
			t.Fatal("unsafe migration accepted for analysis")
		}
	}
}

func TestDecodePlanRejectsMalformedAndUnboundedMetrics(t *testing.T) {
	for _, raw := range []string{"null", "[]", "[{}]", `[{"Plan":{"Node Type":"Seq Scan","Total Cost":-1}}]`, `[{"Plan":{"Node Type":"Seq Scan","Plans":[{}]}}]`} {
		if _, err := DecodePlan([]byte(raw), false); err == nil {
			t.Fatalf("malformed plan accepted: %s", raw)
		}
	}
	if _, err := DecodePlan([]byte(strings.Repeat(" ", MaxPlanBytes+1)), false); err == nil {
		t.Fatal("oversized plan accepted")
	}
	raw := []byte(`[{"Plan":{"Node Type":"Seq Scan","Startup Cost":0,"Total Cost":10,"Plan Rows":20,"Plan Width":8},"Planning Time":0.1}]`)
	if result, err := DecodePlan(raw, false); err != nil || result.Plan.Rows != 20 || result.ExecutionMS != nil {
		t.Fatal("valid estimated plan rejected")
	}
	if _, err := DecodePlan(raw, true); err == nil {
		t.Fatal("ANALYZE accepted without actual statistics")
	}
}
