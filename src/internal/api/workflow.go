package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	workflowMigrations = "/databases/{database_id}/migrations"
	workflowChanges    = "/databases/{database_id}/change-requests"
	workflowChange     = workflowChanges + "/{change_request_id}"
	workflowJob        = "/jobs/{job_id}"
	workflowExecutions = "/databases/{database_id}/executions"
)

// WorkflowRoutes registers the draft-to-job API. Append to Config.Routes.
func WorkflowRoutes(service *migration.WorkflowService) ([]Route, error) {
	if service == nil {
		return nil, fmt.Errorf("workflow service is required: %w", security.ErrInvalidInput)
	}
	h := workflowEndpoints{service}
	return []Route{
		{Method: http.MethodGet, Path: workflowMigrations, QueryParameters: []string{"page", "limit"}, Handle: h.definitions},
		{Method: http.MethodPost, Path: workflowMigrations, Body: JSONBody, Handle: h.writeDefinition},
		{Method: http.MethodGet, Path: workflowMigrations + "/{migration_id}", Handle: h.definition},
		{Method: http.MethodPatch, Path: workflowMigrations + "/{migration_id}", Body: JSONBody, Handle: h.writeDefinition},
		{Method: http.MethodGet, Path: workflowChanges, QueryParameters: []string{"page", "limit", "status"}, Handle: h.changes},
		{Method: http.MethodPost, Path: workflowChanges, Body: JSONBody, Handle: h.submit},
		{Method: http.MethodGet, Path: workflowChange, Handle: h.change},
		{Method: http.MethodPost, Path: workflowChange + "/approvals", Body: JSONBody, Handle: h.decide},
		{Method: http.MethodPost, Path: workflowChange + "/execute", Body: JSONBody, Handle: h.enqueue},
		{Method: http.MethodGet, Path: workflowJob, Handle: h.job},
		{Method: http.MethodGet, Path: workflowJob + "/attempts", QueryParameters: []string{"page", "limit"}, Handle: h.jobAttempts},
		{Method: http.MethodGet, Path: "/databases/{database_id}/jobs", QueryParameters: []string{"page", "limit", "status"}, Handle: h.jobs},
		{Method: http.MethodGet, Path: workflowExecutions, QueryParameters: []string{"page", "limit", "hide_rollback"}, Handle: h.executions},
		{Method: http.MethodGet, Path: workflowExecutions + "/{execution_id}", Handle: h.execution},
		{Method: http.MethodGet, Path: workflowExecutions + "/{execution_id}/statements", QueryParameters: []string{"page", "limit"}, Handle: h.statements},
		{Method: http.MethodGet, Path: "/databases/{database_id}/position", Handle: h.position},
	}, nil
}

type workflowEndpoints struct{ service *migration.WorkflowService }

func workflowRequest(r *http.Request) (migration.Request, error) {
	actor, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return migration.Request{}, security.ErrUnauthenticated
	}
	id, err := PathID(r, "database_id")
	return migration.Request{Principal: actor, DatabaseID: id}, err
}

func workflowPage(r *http.Request) (migration.PageQuery, error) {
	page, limit, _, err := explorer.ParsePagination(r.URL.Query())
	if err != nil {
		return migration.PageQuery{}, security.ErrMalformedRequest
	}
	return migration.PageQuery{Page: page, Limit: limit}, nil
}

func writeWorkflowPage[T, D any](w http.ResponseWriter, page migration.Page[T], project func(T) D) error {
	items := make([]D, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, project(item))
	}
	return WriteJSON(w, http.StatusOK, struct {
		pageInfo
		Items []D `json:"items"`
	}{pageInfo: pageInfo{Page: page.Page, Limit: page.Limit, HasNext: page.HasNext}, Items: items})
}

func (h workflowEndpoints) definitions(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	result, err := h.service.ListDefinitions(r.Context(), req, page)
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, definitionSummaryResponse)
}

func (h workflowEndpoints) definition(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	id, err := PathID(r, "migration_id")
	if err != nil {
		return err
	}
	result, err := h.service.FindDefinition(r.Context(), req, id)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, definitionResponse(result))
}

func (h workflowEndpoints) writeDefinition(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	if r.Method == http.MethodPost {
		var input definitionFields
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		draft, err := input.domain()
		if err != nil {
			return err
		}
		result, err := h.service.CreateDefinition(r.Context(), req, draft)
		if err != nil {
			return workflowAPIError(err)
		}
		w.Header().Set("Location", Prefix+"/databases/"+string(req.DatabaseID)+"/migrations/"+string(result.ID))
		return WriteJSON(w, http.StatusCreated, definitionResponse(result))
	}
	id, err := PathID(r, "migration_id")
	if err != nil {
		return err
	}
	var input struct {
		definitionFields
		Revision uint64 `json:"revision" json_required:"true"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	draft, err := input.domain()
	if err != nil {
		return err
	}
	result, err := h.service.UpdateDefinition(r.Context(), req, id, input.Revision, draft)
	if err != nil {
		return workflowAPIError(err)
	}
	return WriteJSON(w, http.StatusOK, definitionResponse(result))
}

func (h workflowEndpoints) changes(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	var status migration.CRStatus
	if values := r.URL.Query(); values.Has("status") {
		status = migration.CRStatus(values.Get("status"))
		switch status {
		case migration.CRPendingReview, migration.CRApproved, migration.CRRejected, migration.CRQueued, migration.CRRunning,
			migration.CRSucceeded, migration.CRFailed, migration.CRPartial, migration.CRUncertain, migration.CRPartiallyReverted:
		default:
			return security.ErrMalformedRequest
		}
	}
	result, err := h.service.ListChangeRequests(r.Context(), req, migration.ChangeRequestQuery{PageQuery: page, Status: status})
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, changeResponse)
}

func (h workflowEndpoints) change(w http.ResponseWriter, r *http.Request) error {
	req, id, err := workflowChangeRequest(r)
	if err != nil {
		return err
	}
	result, err := h.service.FindChangeRequest(r.Context(), req, id)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, changeResponse(result))
}

func (h workflowEndpoints) submit(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	var input struct {
		MigrationID       security.ID          `json:"migration_id" json_required:"true"`
		Revision          uint64               `json:"migration_revision" json_required:"true"`
		Kind              migration.ChangeKind `json:"kind" json_required:"true"`
		TargetExecutionID security.ID          `json:"target_execution_id"`
		SourceJobID       security.ID          `json:"source_job_id"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	result, err := h.service.Submit(r.Context(), req, migration.SubmitChange{MigrationID: input.MigrationID, MigrationRevision: input.Revision, Kind: input.Kind,
		TargetExecutionID: input.TargetExecutionID, SourceJobID: input.SourceJobID})
	if err != nil {
		return workflowAPIError(err)
	}
	w.Header().Set("Location", Prefix+"/databases/"+string(req.DatabaseID)+"/change-requests/"+string(result.ID))
	return WriteJSON(w, http.StatusCreated, changeResponse(result))
}

func (h workflowEndpoints) decide(w http.ResponseWriter, r *http.Request) error {
	req, id, err := workflowChangeRequest(r)
	if err != nil {
		return err
	}
	var input struct {
		Revision uint64             `json:"revision" json_required:"true"`
		GroupID  security.ID        `json:"group_id" json_required:"true"`
		Decision migration.Decision `json:"decision" json_required:"true"`
		Note     string             `json:"note"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	result, err := h.service.Decide(r.Context(), req, id, input.Revision, input.GroupID, input.Decision, input.Note)
	if err != nil {
		return workflowAPIError(err)
	}
	return WriteJSON(w, http.StatusOK, changeResponse(result))
}

func (h workflowEndpoints) enqueue(w http.ResponseWriter, r *http.Request) error {
	req, id, err := workflowChangeRequest(r)
	if err != nil {
		return err
	}
	var input struct {
		Revision uint64 `json:"revision" json_required:"true"`
	}
	if err := DecodeJSON(w, r, &input); err != nil {
		return err
	}
	result, err := h.service.Enqueue(r.Context(), req, id, input.Revision)
	if err != nil {
		return workflowAPIError(err)
	}
	w.Header().Set("Location", Prefix+"/jobs/"+string(result.ID))
	return WriteJSON(w, http.StatusAccepted, struct {
		ID     security.ID         `json:"job_id"`
		Status migration.JobStatus `json:"status"`
	}{ID: result.ID, Status: result.Status})
}

func (h workflowEndpoints) job(w http.ResponseWriter, r *http.Request) error {
	actor, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return security.ErrUnauthenticated
	}
	id, err := PathID(r, "job_id")
	if err != nil {
		return err
	}
	result, err := h.service.FindJob(r.Context(), actor, id)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, jobResponse(result))
}

func workflowChangeRequest(r *http.Request) (migration.Request, security.ID, error) {
	req, err := workflowRequest(r)
	if err != nil {
		return req, "", err
	}
	id, err := PathID(r, "change_request_id")
	return req, id, err
}

func workflowAPIError(err error) error {
	var validation migration.WorkflowValidation
	if errors.As(err, &validation) {
		switch validation {
		case migration.WorkflowPostgreSQLOnly, migration.WorkflowUnsupportedChange, migration.WorkflowMissingLeader, migration.WorkflowNoGroups, migration.WorkflowIrreversible:
			return &ValidationError{Fields: map[string][]string{"kind": {validation.Error()}}}
		}
	}
	var sql migration.SQLRejection
	if errors.As(err, &sql) {
		return &ValidationError{Fields: map[string][]string{"sql": {sql.Message()}}}
	}
	return err
}
