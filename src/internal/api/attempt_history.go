package api

import (
	"net/http"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type historyResponse struct {
	Checksum             string                        `json:"checksum"`
	ActorID              *security.ID                  `json:"actor_id"`
	ID                   security.ID                   `json:"id"`
	DatabaseID           security.ID                   `json:"database_id"`
	MigrationID          security.ID                   `json:"migration_id"`
	ChangeRequestID      *security.ID                  `json:"change_request_id"`
	JobID                *security.ID                  `json:"job_id"`
	ExecutionID          security.ID                   `json:"execution_id"`
	RetryOfJobID         *security.ID                  `json:"retry_of_job_id"`
	Attempt              int                           `json:"attempt"`
	Revision             uint64                        `json:"migration_revision"`
	Operation            migration.ChangeKind          `json:"operation"`
	Direction            *migration.ExecutionDirection `json:"direction"`
	Status               migration.JobStatus           `json:"status"`
	ResolvedStatus       migration.JobStatus           `json:"resolved_status"`
	Actor                string                        `json:"actor"`
	StartedAt            time.Time                     `json:"started_at"`
	FinishedAt           *time.Time                    `json:"finished_at"`
	CommittedAt          *time.Time                    `json:"committed_at"`
	PositionVersion      *migration.PositionVersion    `json:"position_version"`
	CanRetry             bool                          `json:"can_retry"`
	RevertsExecutionID   *security.ID                  `json:"reverts_execution_id"`
	ForwardOfExecutionID *security.ID                  `json:"forward_of_execution_id"`
	Error                *workflowJobFailure           `json:"error"`
	SnapshotAvailable    bool                          `json:"snapshot_available"`
	UpSQL                *string                       `json:"up_sql"`
	DownSQL              *string                       `json:"down_sql"`
	SQLSnapshot          *string                       `json:"sql_snapshot"`
}

func historyDTO(e migration.HistoryEntry) historyResponse {
	r := historyResponse{Checksum: e.Checksum, ActorID: optionalValue(e.ActorID), ID: e.ID, DatabaseID: e.DatabaseID, MigrationID: e.MigrationID, ChangeRequestID: optionalValue(e.ChangeRequestID), JobID: optionalValue(e.JobID), ExecutionID: e.ExecutionID,
		RetryOfJobID: optionalValue(e.RetryOfJobID), Attempt: e.Attempt, Revision: e.Revision, Operation: e.Operation, Direction: optionalValue(e.Direction),
		Status: e.Status, ResolvedStatus: e.ResolvedStatus, Actor: e.Actor, StartedAt: e.StartedAt.UTC(), FinishedAt: e.FinishedAt, CommittedAt: e.CommittedAt, PositionVersion: e.PositionVersion, CanRetry: e.CanRetry,
		RevertsExecutionID: optionalValue(e.RevertsExecutionID), ForwardOfExecutionID: optionalValue(e.ForwardOfExecutionID), Error: jobFailureResponse(e.Error), SnapshotAvailable: e.SnapshotAvailable || e.Definition != nil}
	if e.Definition != nil {
		up := e.Definition.UpSQL.Reveal()
		r.UpSQL = &up
		r.DownSQL = optionalValue(e.Definition.DownSQL.Reveal())
		switch e.Direction {
		case migration.DirectionUp:
			r.SQLSnapshot = &up
		case migration.DirectionDown:
			r.SQLSnapshot = r.DownSQL
		}
	}
	return r
}

func (h workflowEndpoints) history(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	hide := r.URL.Query().Get("hide_rollback")
	if hide != "" && hide != "true" && hide != "false" {
		return security.ErrMalformedRequest
	}
	result, err := h.service.ListHistory(r.Context(), req, migration.ExecutionQuery{PageQuery: page, HideRollback: hide == "true"})
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, historyDTO)
}
func (h workflowEndpoints) historyEntry(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	id, err := PathID(r, "history_id")
	if err != nil {
		return err
	}
	result, err := h.service.FindHistory(r.Context(), req, id)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, historyDTO(result))
}

type historyStatementResponse struct {
	workflowExecutedStatement
	Ordinal    int64      `json:"ordinal"`
	Status     string     `json:"status"`
	SQLState   *string    `json:"sqlstate"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func (h workflowEndpoints) historyStatements(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	id, err := PathID(r, "history_id")
	if err != nil {
		return err
	}
	page, err := workflowPage(r)
	if err != nil {
		return err
	}
	result, err := h.service.HistoryStatements(r.Context(), req, id, page)
	if err != nil {
		return err
	}
	return writeWorkflowPage(w, result, func(v migration.HistoryStatement) historyStatementResponse {
		return historyStatementResponse{workflowExecutedStatement: executedStatementResponse(v.ExecutedStatement), Ordinal: v.Ordinal, Status: v.Status,
			SQLState: optionalValue(v.SQLState), StartedAt: v.StartedAt.UTC(), FinishedAt: v.FinishedAt}
	})
}
func (h workflowEndpoints) retryJob(w http.ResponseWriter, r *http.Request) error {
	req, err := workflowRequest(r)
	if err != nil {
		return err
	}
	id, err := PathID(r, "job_id")
	if err != nil {
		return err
	}
	var body struct{}
	if err := DecodeJSON(w, r, &body); err != nil {
		return err
	}
	result, err := h.service.RetryFailedJob(r.Context(), req, id)
	if err != nil {
		return workflowAPIError(err)
	}
	w.Header().Set("Location", Prefix+"/databases/"+string(req.DatabaseID)+"/change-requests/"+string(result.ID))
	return WriteJSON(w, http.StatusCreated, changeResponse(result))
}
