package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type MigrationPlanService interface {
	SupportsPlans() bool
	Analyze(context.Context, DatabaseRequest, security.ID, migration.PlanOptions) (migration.PlanResult, error)
}
type planLine struct{ Prefix, Type, Relation, Index, Join, Startup, Cost, Rows, ActualStartup, ActualTotal, ActualRows, Loops string }
type planView struct {
	DetailHref          string
	Mode                string
	Statement           int
	Revision            uint64
	Checksum            string
	Planning, Execution string
	Lines               []planLine
	JSON                string
	SQL                 string
}

func planMetric(value *float64) string {
	if value == nil {
		return "-"
	}
	return strconv.FormatFloat(*value, 'f', 3, 64)
}
func flattenPlan(node migration.PlanNode, depth int, result *[]planLine) {
	join := strings.TrimSpace(node.Join + " " + node.Strategy)
	if join == "" {
		join = "-"
	}
	relation := node.Relation
	if node.Schema != "" {
		relation = node.Schema + "." + relation
	}
	if relation == "" {
		relation = "-"
	}
	index := node.Index
	if index == "" {
		index = "-"
	}
	*result = append(*result, planLine{Prefix: strings.Repeat("  ", depth), Type: node.NodeType, Relation: relation, Index: index, Join: join,
		Startup: fmt.Sprintf("%.3f", node.StartupCost), Cost: fmt.Sprintf("%.3f", node.TotalCost), Rows: fmt.Sprintf("%.0f", node.Rows), ActualStartup: planMetric(node.ActualStartup), ActualTotal: planMetric(node.ActualTotal), ActualRows: planMetric(node.ActualRows), Loops: planMetric(node.ActualLoops)})
	for _, child := range node.Plans {
		flattenPlan(child, depth+1, result)
	}
}

func (ui *MigrationUI) plan(w http.ResponseWriter, r *http.Request) {
	req, err := ui.access.Authorize(r, security.ActionAnalyzeMigration)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	id, err := pathID(r, "migrationID")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	values, err := readForm(r, "revision", "statement", "mode")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	revision, err := formRevision(values)
	if err != nil {
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	statement, err := strconv.Atoi(values.Get("statement"))
	mode := values.Get("mode")
	if err != nil || statement < 1 || statement > 1000 || strconv.Itoa(statement) != values.Get("statement") || (mode != "explain" && mode != "analyze") {
		_ = ui.renderer.RenderError(w, r, security.ErrInvalidInput)
		return
	}
	result, err := ui.plans.Analyze(r.Context(), req, id, migration.PlanOptions{Revision: revision, Statement: statement, Analyze: mode == "analyze"})
	if err != nil {
		var reason migration.PlanRejection
		if errors.As(err, &reason) {
			status := http.StatusBadRequest
			if reason == "timeout" {
				status = http.StatusRequestTimeout
			}
			if reason == "large" {
				status = http.StatusUnprocessableEntity
			}
			_ = ui.renderer.Render(w, r, status, "migration/rejected", Page{Title: "Analisis query ditolak", Content: MigrationRejectedView{DetailHref: databasePath(req.DatabaseID, "migrations", string(id)), Message: reason.Message()}})
			return
		}
		if status, message, ok := formFailure(err); ok {
			_ = ui.renderer.Render(w, r, status, "migration/rejected", Page{Title: "Analisis query ditolak", Content: MigrationRejectedView{DetailHref: databasePath(req.DatabaseID, "migrations", string(id)), Message: message}})
			return
		}
		_ = ui.renderer.RenderError(w, r, err)
		return
	}
	view := planView{DetailHref: databasePath(req.DatabaseID, "migrations", string(id)), Mode: "EXPLAIN", Statement: result.Statement, Revision: result.Revision, Checksum: fmt.Sprintf("%x", result.Checksum), Planning: planMetric(result.PlanningMS), Execution: planMetric(result.ExecutionMS)}
	view.SQL = result.SQL.Reveal()
	if result.Analyze {
		view.Mode = "EXPLAIN ANALYZE"
	}
	flattenPlan(result.Plan, 0, &view.Lines)
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		_ = ui.renderer.RenderError(w, r, security.ErrUnavailable)
		return
	}
	view.JSON = string(raw)
	_ = ui.renderer.Render(w, r, http.StatusOK, "migration/plan", Page{Title: "Execution plan", Content: view})
}
