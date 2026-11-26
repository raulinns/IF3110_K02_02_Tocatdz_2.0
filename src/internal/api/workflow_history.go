package api

import (
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func (h workflowEndpoints) executions(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	query := migration.ExecutionQuery{PageQuery: page}
	if values := r.URL.Query(); values.Has("hide_rollback") {
		switch values.Get("hide_rollback") {
		case "true":
			query.HideRollback = true
		case "false":
		default:
			return security.ErrMalformedRequest
		}
	}
	result, err := h.service.ListExecutions(r.Context(), req, query)
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, executionSummaryResponse)
}

func (h workflowEndpoints) execution(w http.ResponseWriter, r *http.Request) error {
	req, id, err := workflowExecution(r)
	if err != nil {
		return err
	}
	result, err := h.service.FindExecution(r.Context(), req, id)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, executionResponse(result))
}

func (h workflowEndpoints) statements(w http.ResponseWriter, r *http.Request) error {
	req, id, err := workflowExecution(r)
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	result, err := h.service.ListExecutedStatements(r.Context(), req, id, page)
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, executedStatementResponse)
}

func (h workflowEndpoints) position(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	result, err := h.service.Position(r.Context(), req)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, positionResponse(result))
}

func (h workflowEndpoints) jobs(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	query := migration.JobQuery{PageQuery: page}
	if values := r.URL.Query(); values.Has("status") {
		query.Status = migration.JobStatus(values.Get("status"))
		switch query.Status {
		case migration.JobQueued, migration.JobRunning, migration.JobSucceeded, migration.JobFailed,
			migration.JobPartial, migration.JobUncertain, migration.JobPartiallyReverted:
		default:
			return security.ErrMalformedRequest
		}
	}
	result, err := h.service.ListJobs(r.Context(), req, query)
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, jobResponse)
}

func (h workflowEndpoints) jobAttempts(w http.ResponseWriter, r *http.Request) error {
	actor, ok := security.PrincipalFromContext(r.Context())
	if !ok {
		return security.ErrUnauthenticated
	}
	id, err := PathID(r, "job_id")
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	result, err := h.service.ListAttempts(r.Context(), actor, id, page)
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, jobAttemptResponse)
}

func workflowExecution(r *http.Request) (migration.Request, security.ID, error) {
	req, err := workflowRequest(r)
	if err != nil {
		return req, "", err
	}
	id, err := PathID(r, "execution_id")
	return req, id, err
}
