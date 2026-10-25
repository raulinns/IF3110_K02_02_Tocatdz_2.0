package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type fakePlanMigrations struct {
	fakeMigrations
	options migration.PlanOptions
	planErr error
	plans   int
}

func (*fakePlanMigrations) SupportsPlans() bool { return true }
func (f *fakePlanMigrations) Analyze(_ context.Context, _ DatabaseRequest, _ security.ID, options migration.PlanOptions) (migration.PlanResult, error) {
	f.options = options
	f.plans++
	return migration.PlanResult{Plan: migration.PlanNode{NodeType: "<script>alert(1)</script>", Rows: 1}, Statement: options.Statement, Revision: options.Revision}, f.planErr
}

func TestPlanHandlerEscapesOutputAndValidatesForm(t *testing.T) {
	plans := &fakePlanMigrations{fakeMigrations: fakeMigrations{detail: draftDetail()}}
	renderer, _ := NewRenderer()
	access, _ := NewDatabaseGuard(&fakeAuthorizer{})
	ui, err := NewMigrationUI(plans, &fakeGuard{}, access, renderer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ui.RegisterRoutes(mux)
	post := func(values url.Values, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", migrationPath+"/plan", strings.NewReader(values.Encode()))
		r = r.WithContext(security.WithPrincipal(r.Context(), testPrincipal))
		r.PostForm = values
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("got %d want %d", w.Code, want)
		}
		return w
	}
	values := url.Values{"revision": {"3"}, "statement": {"1"}, "mode": {"explain"}}
	w := post(values, 200)
	if strings.Contains(w.Body.String(), "<script>alert") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatal("plan metadata is not escaped")
	}
	for _, field := range []string{"mode", "statement", "revision"} {
		bad := url.Values{}
		for k, v := range values {
			bad[k] = append([]string{}, v...)
		}
		bad[field] = []string{"invalid"}
		post(bad, 400)
	}
	if plans.plans != 1 {
		t.Fatal("invalid form reached analysis service")
	}
	plans.planErr = migration.PlanRejection("timeout")
	post(values, http.StatusRequestTimeout)
}
