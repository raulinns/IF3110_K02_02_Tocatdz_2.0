package api

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type DefinitionPlanner interface {
	AnalyzeDefinition(context.Context, migration.Request, security.ID, migration.DefinitionPlanOptions) (migration.PlanResult, error)
}

// QueryPlanRoutes mounts the saved-definition planner as part of Config.Routes.
func QueryPlanRoutes(planner DefinitionPlanner) ([]Route, error) {
	if missingAuthDependency(planner) {
		return nil, fmt.Errorf("definition planner is required: %w", security.ErrInvalidInput)
	}
	return []Route{{
		Method: http.MethodPost,
		Path:   "/databases/{database_id}/migrations/{migration_id}/plan",
		Body:   JSONBody,
		Handle: queryPlan(planner),
	}}, nil
}

type queryPlanInput struct {
	Revision  uint64                       `json:"revision"`
	Direction migration.ExecutionDirection `json:"direction"`
	Statement int                          `json:"statement"`
	Analyze   *bool                        `json:"analyze"`
}

func queryPlan(planner DefinitionPlanner) Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		actor, ok := security.PrincipalFromContext(r.Context())
		if !ok {
			return security.ErrUnauthenticated
		}
		databaseID, err := PathID(r, "database_id")
		if err != nil {
			return err
		}
		migrationID, err := PathID(r, "migration_id")
		if err != nil {
			return err
		}
		var input queryPlanInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Analyze == nil {
			return &ValidationError{Fields: map[string][]string{"analyze": {"Pilih true atau false."}}}
		}
		result, err := planner.AnalyzeDefinition(r.Context(), migration.Request{DatabaseID: databaseID, Principal: actor},
			migrationID, migration.DefinitionPlanOptions{
				PlanOptions: migration.PlanOptions{Revision: input.Revision, Statement: input.Statement, Analyze: *input.Analyze},
				Direction:   input.Direction,
			})
		if err != nil {
			return planError(err)
		}
		return WriteJSON(w, http.StatusOK, queryPlanResponse{
			Revision: result.Revision, Checksum: hex.EncodeToString(result.Checksum[:]), Direction: input.Direction,
			Statement: result.Statement, Analyze: result.Analyze, Plan: planNodeResponse(result.Plan),
			PlanningMS: result.PlanningMS, ExecutionMS: result.ExecutionMS,
		})
	}
}

func planError(err error) error {
	var rejection migration.PlanRejection
	if !errors.As(err, &rejection) {
		return err
	}
	field := "statement"
	switch rejection {
	case "large":
		return security.ErrResultTooLarge
	case "timeout":
		return security.ErrUnavailable
	case "analyze":
		field = "analyze"
	case "direction":
		field = "direction"
	}
	return &ValidationError{Fields: map[string][]string{field: {rejection.Message()}}}
}

type queryPlanResponse struct {
	Revision    uint64                       `json:"revision"`
	Checksum    string                       `json:"checksum"`
	Direction   migration.ExecutionDirection `json:"direction"`
	Statement   int                          `json:"statement"`
	Analyze     bool                         `json:"analyze"`
	Plan        queryPlanNode                `json:"plan"`
	PlanningMS  *float64                     `json:"planning_ms"`
	ExecutionMS *float64                     `json:"execution_ms"`
}

type queryPlanNode struct {
	NodeType        string          `json:"node_type"`
	Relation        string          `json:"relation,omitempty"`
	Schema          string          `json:"schema,omitempty"`
	Index           string          `json:"index,omitempty"`
	Join            string          `json:"join,omitempty"`
	Strategy        string          `json:"strategy,omitempty"`
	StartupCost     float64         `json:"startup_cost"`
	TotalCost       float64         `json:"total_cost"`
	Rows            float64         `json:"rows"`
	Width           float64         `json:"width"`
	ActualStartupMS *float64        `json:"actual_startup_ms,omitempty"`
	ActualTotalMS   *float64        `json:"actual_total_ms,omitempty"`
	ActualRows      *float64        `json:"actual_rows,omitempty"`
	ActualLoops     *float64        `json:"actual_loops,omitempty"`
	Plans           []queryPlanNode `json:"plans,omitempty"`
}

func planNodeResponse(node migration.PlanNode) queryPlanNode {
	result := queryPlanNode{
		NodeType: node.NodeType, Relation: node.Relation, Schema: node.Schema, Index: node.Index, Join: node.Join, Strategy: node.Strategy,
		StartupCost: node.StartupCost, TotalCost: node.TotalCost, Rows: node.Rows, Width: node.Width,
		ActualStartupMS: node.ActualStartup, ActualTotalMS: node.ActualTotal, ActualRows: node.ActualRows, ActualLoops: node.ActualLoops,
	}
	if len(node.Plans) > 0 {
		result.Plans = make([]queryPlanNode, len(node.Plans))
		for i, child := range node.Plans {
			result.Plans[i] = planNodeResponse(child)
		}
	}
	return result
}
