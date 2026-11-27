package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// M2Store mengimplementasikan port persistence workflow di database platform. Store tidak melakukan otorisasi.
type M2Store struct{ pool *pgxpool.Pool }

var (
	_ migration.DefinitionStore    = (*M2Store)(nil)
	_ migration.ChangeRequestStore = (*M2Store)(nil)
	_ migration.ExecutionStore     = (*M2Store)(nil)
	_ migration.JobQueue           = (*M2Store)(nil)
	_ migration.JobLocator         = (*M2Store)(nil)
	_ migration.LeaderDirectory    = (*M2Store)(nil)
	_ migration.WorkflowStore      = (*M2Store)(nil)
)

func NewM2Store(pool *pgxpool.Pool) (*M2Store, error) {
	if pool == nil {
		return nil, security.ErrInvalidInput
	}
	return &M2Store{pool: pool}, nil
}

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
	maxPageOffset    = 1_000_000
)

func pageWindow(query migration.PageQuery) (migration.PageQuery, int, error) {
	if query.Page == 0 {
		query.Page = 1
	}
	if query.Limit == 0 {
		query.Limit = defaultPageLimit
	}
	if query.Page < 1 || query.Limit < 1 || query.Limit > maxPageLimit {
		return query, 0, security.ErrInvalidInput
	}
	offset := (query.Page - 1) * query.Limit
	if offset > maxPageOffset {
		return query, 0, security.ErrInvalidInput
	}
	return query, offset, nil
}

func finishPage[T any](items []T, query migration.PageQuery) migration.Page[T] {
	hasNext := len(items) > query.Limit
	if hasNext {
		items = items[:query.Limit]
	}
	if items == nil {
		items = []T{}
	}
	return migration.Page[T]{Items: items, Page: query.Page, Limit: query.Limit, HasNext: hasNext}
}

func collect[T any](rows pgx.Rows, err error, scan func(pgx.Rows) (T, error)) ([]T, error) {
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func nullID(id security.ID) any {
	if id == "" {
		return nil
	}
	return string(id)
}

func optionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := utc(*value)
	return &converted
}

func checksumOf(raw []byte) migration.DefinitionChecksum {
	var checksum migration.DefinitionChecksum
	if len(raw) == len(checksum) {
		copy(checksum[:], raw)
	}
	return checksum
}

type batchJSON struct {
	Schema     string  `json:"schema"`
	Table      string  `json:"table"`
	KeyColumn  string  `json:"key_column"`
	BatchSize  int     `json:"batch_size"`
	ScopeWhere *string `json:"scope_where"`
}

func encodeBatch(config *migration.BatchConfig) (any, error) {
	if config == nil {
		return nil, nil
	}
	value := batchJSON{Schema: config.Schema, Table: config.Table, KeyColumn: config.KeyColumn, BatchSize: config.BatchSize}
	if !config.ScopeWhere.Empty() {
		scope := config.ScopeWhere.Reveal()
		value.ScopeWhere = &scope
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, security.ErrInvalidInput
	}
	return string(raw), nil
}

func decodeBatch(raw []byte) (*migration.BatchConfig, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value batchJSON
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, security.ErrUnavailable
	}
	config := &migration.BatchConfig{Schema: value.Schema, Table: value.Table, KeyColumn: value.KeyColumn, BatchSize: value.BatchSize}
	if value.ScopeWhere != nil {
		config.ScopeWhere = security.NewSecret(*value.ScopeWhere)
	}
	return config, nil
}

type statementJSON struct {
	Query  string   `json:"query"`
	Params []string `json:"params"`
	Batch  *int64   `json:"batch"`
}

func encodeStatements(statements []migration.ExecutedStatement) (string, error) {
	values := make([]statementJSON, len(statements))
	for i, statement := range statements {
		params := make([]string, len(statement.Params))
		for j, param := range statement.Params {
			params[j] = param.Reveal()
		}
		values[i] = statementJSON{Query: statement.Query.Reveal(), Params: params, Batch: statement.Batch}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", security.ErrInvalidInput
	}
	return string(raw), nil
}

func decodeStatement(raw []byte) (migration.ExecutedStatement, error) {
	var value statementJSON
	if err := json.Unmarshal(raw, &value); err != nil {
		return migration.ExecutedStatement{}, security.ErrUnavailable
	}
	params := make([]security.Secret, len(value.Params))
	for i, param := range value.Params {
		params[i] = security.NewSecret(param)
	}
	return migration.ExecutedStatement{Query: security.NewSecret(value.Query), Params: params, Batch: value.Batch}, nil
}

func decodeStatements(raw []byte) ([]migration.ExecutedStatement, error) {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, security.ErrUnavailable
	}
	result := make([]migration.ExecutedStatement, len(values))
	for i, value := range values {
		statement, err := decodeStatement(value)
		if err != nil {
			return nil, err
		}
		result[i] = statement
	}
	return result, nil
}

const definitionColumns = `m.id::text, m.database_id::text, m.title, m.sql, m.down_sql, m.irreversible, m.irreversible_reason, m.mode,
	m.batch_config, m.definition_checksum, m.frozen_at, m.first_execution_at, m.status, m.revision, m.created_by::text, m.created_at, m.updated_at`

// Migration M1 yang sudah dieksekusi juga dianggap beku.
func definitionState(status string, frozen *time.Time) migration.DefinitionState {
	if frozen != nil || status != "draft" {
		return migration.DefinitionFrozen
	}
	return migration.DefinitionDraft
}

func scanDefinition(row pgx.Row) (migration.MigrationDefinition, error) {
	var d migration.MigrationDefinition
	var id, database, up, mode, status, createdBy string
	var down *string
	var batch, checksum []byte
	var frozen, first *time.Time
	var revision int64
	if err := row.Scan(&id, &database, &d.Title, &up, &down, &d.Irreversible, &d.IrreversibleReason, &mode, &batch, &checksum,
		&frozen, &first, &status, &revision, &createdBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return migration.MigrationDefinition{}, mapError(err)
	}
	config, err := decodeBatch(batch)
	if err != nil {
		return migration.MigrationDefinition{}, err
	}
	d.ID, d.DatabaseID, d.CreatedBy = security.ID(id), security.ID(database), security.ID(createdBy)
	d.UpSQL, d.Mode, d.Batch = security.NewSecret(up), migration.ExecutionMode(mode), config
	if down != nil {
		d.DownSQL = security.NewSecret(*down)
	}
	d.State, d.Revision, d.Checksum = definitionState(status, frozen), uint64(revision), checksumOf(checksum)
	d.CreatedAt, d.UpdatedAt, d.FirstExecutionAt = utc(d.CreatedAt), utc(d.UpdatedAt), optionalTime(first)
	return d, nil
}

func findDefinition(ctx context.Context, q querier, database, id security.ID, suffix string) (migration.MigrationDefinition, error) {
	if !security.ValidID(database) || !security.ValidID(id) {
		return migration.MigrationDefinition{}, security.ErrNotFound
	}
	return scanDefinition(q.QueryRow(ctx, "SELECT "+definitionColumns+" FROM migrations m WHERE m.id = $1 AND m.database_id = $2"+suffix,
		string(id), string(database)))
}

func (s *M2Store) FindDefinition(ctx context.Context, databaseID, migrationID security.ID) (migration.MigrationDefinition, error) {
	return findDefinition(ctx, s.pool, databaseID, migrationID, "")
}

func (s *M2Store) ListDefinitions(ctx context.Context, databaseID security.ID, query migration.PageQuery) (migration.Page[migration.MigrationDefinitionSummary], error) {
	query, offset, err := pageWindow(query)
	if err != nil || !security.ValidID(databaseID) {
		return migration.Page[migration.MigrationDefinitionSummary]{}, security.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, database_id::text, title, status, frozen_at, revision, definition_checksum, mode,
		irreversible, created_by::text, created_at, updated_at, first_execution_at
		FROM migrations WHERE database_id = $1 ORDER BY updated_at DESC, id DESC LIMIT $2 OFFSET $3`, string(databaseID), query.Limit+1, offset)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.MigrationDefinitionSummary, error) {
		var s migration.MigrationDefinitionSummary
		var id, database, status, mode, createdBy string
		var frozen, first *time.Time
		var revision int64
		var checksum []byte
		if err := rows.Scan(&id, &database, &s.Title, &status, &frozen, &revision, &checksum, &mode, &s.Irreversible, &createdBy,
			&s.CreatedAt, &s.UpdatedAt, &first); err != nil {
			return s, mapError(err)
		}
		s.ID, s.DatabaseID, s.CreatedBy, s.Mode = security.ID(id), security.ID(database), security.ID(createdBy), migration.ExecutionMode(mode)
		s.State, s.Revision, s.Checksum = definitionState(status, frozen), uint64(revision), checksumOf(checksum)
		s.CreatedAt, s.UpdatedAt, s.FirstExecutionAt = utc(s.CreatedAt), utc(s.UpdatedAt), optionalTime(first)
		return s, nil
	})
	if err != nil {
		return migration.Page[migration.MigrationDefinitionSummary]{}, err
	}
	return finishPage(items, query), nil
}

const changeRequestColumns = `c.id::text, c.database_id::text, c.migration_id::text, c.migration_revision, c.kind,
	COALESCE(c.target_execution_id::text, ''), COALESCE(c.source_job_id::text, ''), c.revision, c.checksum, c.status,
	c.requested_by::text, c.expected_position_version, c.created_at, c.updated_at, COALESCE(c.retry_of_job_id::text,'')`

func scanChangeRequest(row pgx.Row) (migration.ChangeRequest, error) {
	var c migration.ChangeRequest
	var id, database, migrationID, kind, target, source, status, requestedBy string
	var migrationRevision, revision, position int64
	var checksum []byte
	if err := row.Scan(&id, &database, &migrationID, &migrationRevision, &kind, &target, &source, &revision, &checksum, &status,
		&requestedBy, &position, &c.CreatedAt, &c.UpdatedAt, &c.RetryOfJobID); err != nil {
		return migration.ChangeRequest{}, mapError(err)
	}
	c.ID, c.DatabaseID, c.MigrationID = security.ID(id), security.ID(database), security.ID(migrationID)
	c.MigrationRevision, c.Kind = uint64(migrationRevision), migration.ChangeKind(kind)
	c.TargetExecutionID, c.SourceJobID = security.ID(target), security.ID(source)
	c.Revision, c.Checksum, c.Status, c.RequestedBy = uint64(revision), checksumOf(checksum), migration.CRStatus(status), security.ID(requestedBy)
	c.ExpectedPosition, c.CreatedAt, c.UpdatedAt = migration.PositionVersion(position), utc(c.CreatedAt), utc(c.UpdatedAt)
	return c, nil
}

func attachApprovals(ctx context.Context, q querier, requests []migration.ChangeRequest) error {
	if len(requests) == 0 {
		return nil
	}
	index := make(map[security.ID]int, len(requests))
	keys := make([]string, len(requests))
	for i, request := range requests {
		index[request.ID], keys[i] = i, string(request.ID)
		requests[i].Approvals = []migration.GroupApproval{}
	}
	rows, err := q.Query(ctx, `SELECT change_request_id::text, group_id::text, leader_user_id::text, group_name, access, decision,
		COALESCE(decided_by::text, ''), note, decided_at FROM change_request_approvals
		WHERE change_request_id = ANY($1::uuid[]) ORDER BY group_name, group_id`, keys)
	if err != nil {
		return mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var request, group, leader, name, access, decidedBy, note string
		var decision *string
		var decidedAt *time.Time
		if err := rows.Scan(&request, &group, &leader, &name, &access, &decision, &decidedBy, &note, &decidedAt); err != nil {
			return mapError(err)
		}
		approval := migration.GroupApproval{RequiredApproval: migration.RequiredApproval{
			GroupID: security.ID(group), LeaderUserID: security.ID(leader), GroupName: name, Access: security.DatabaseAccess(access),
		}}
		if decision != nil && decidedAt != nil {
			approval.Decision = &migration.ApprovalDecision{GroupID: security.ID(group), ActorID: security.ID(decidedBy),
				Decision: migration.Decision(*decision), Note: note, DecidedAt: utc(*decidedAt)}
		}
		i := index[security.ID(request)]
		requests[i].Approvals = append(requests[i].Approvals, approval)
	}
	return mapError(rows.Err())
}

func findChangeRequest(ctx context.Context, q querier, database, id security.ID, suffix string) (migration.ChangeRequest, error) {
	if !security.ValidID(database) || !security.ValidID(id) {
		return migration.ChangeRequest{}, security.ErrNotFound
	}
	request, err := scanChangeRequest(q.QueryRow(ctx, "SELECT "+changeRequestColumns+" FROM change_requests c WHERE c.id = $1 AND c.database_id = $2"+suffix,
		string(id), string(database)))
	if err != nil {
		return migration.ChangeRequest{}, err
	}
	requests := []migration.ChangeRequest{request}
	if err := attachApprovals(ctx, q, requests); err != nil {
		return migration.ChangeRequest{}, err
	}
	return requests[0], nil
}

func (s *M2Store) FindChangeRequest(ctx context.Context, databaseID, changeRequestID security.ID) (migration.ChangeRequest, error) {
	return findChangeRequest(ctx, s.pool, databaseID, changeRequestID, "")
}

func (s *M2Store) ListChangeRequests(ctx context.Context, databaseID security.ID, query migration.ChangeRequestQuery) (migration.Page[migration.ChangeRequest], error) {
	page, offset, err := pageWindow(query.PageQuery)
	if err != nil || !security.ValidID(databaseID) {
		return migration.Page[migration.ChangeRequest]{}, security.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, "SELECT "+changeRequestColumns+` FROM change_requests c
		WHERE c.database_id = $1 AND ($2 = '' OR c.status = $2) ORDER BY c.created_at DESC, c.id DESC LIMIT $3 OFFSET $4`,
		string(databaseID), string(query.Status), page.Limit+1, offset)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.ChangeRequest, error) { return scanChangeRequest(rows) })
	if err != nil {
		return migration.Page[migration.ChangeRequest]{}, err
	}
	result := finishPage(items, page)
	if err := attachApprovals(ctx, s.pool, result.Items); err != nil {
		return migration.Page[migration.ChangeRequest]{}, err
	}
	return result, nil
}

const jobColumns = `j.id::text, j.database_id::text, j.change_request_id::text, j.execution_id::text, j.requested_by::text, j.max_attempts,
	j.request_id, j.trace_context, j.queued_at, c.migration_id::text, c.kind, m.mode, j.status, j.attempt, j.run_after, j.started_at,
	j.finished_at, j.partial, j.batches_committed, j.rows_committed, j.batches_reverted, j.rows_reverted, j.total_batches,
	j.last_error_code, j.last_error_message, j.last_sqlstate, j.last_error_retryable, j.scope_max`

const jobFrom = ` FROM jobs j JOIN change_requests c ON c.id = j.change_request_id JOIN migrations m ON m.id = c.migration_id`

func jobFailure(code *string, message string, sqlstate *string, retryable bool) *migration.JobFailure {
	if code == nil {
		return nil
	}
	failure := &migration.JobFailure{Code: migration.JobErrorCode(*code), Message: message, Retryable: retryable}
	if sqlstate != nil {
		failure.SQLState = *sqlstate
	}
	return failure
}

func scanJob(row pgx.Row) (migration.JobView, error) {
	var j migration.JobView
	var id, database, request, execution, requestedBy, trace, migrationID, kind, mode, status, message string
	var code, sqlstate *string
	var retryable bool
	if err := row.Scan(&id, &database, &request, &execution, &requestedBy, &j.MaxAttempts, &j.RequestID, &trace, &j.QueuedAt,
		&migrationID, &kind, &mode, &status, &j.Attempt, &j.RunAfter, &j.StartedAt, &j.FinishedAt, &j.Partial,
		&j.Progress.BatchesCommitted, &j.Progress.RowsCommitted, &j.Progress.BatchesReverted, &j.Progress.RowsReverted,
		&j.Progress.TotalBatches, &code, &message, &sqlstate, &retryable, &j.ScopeMax); err != nil {
		return migration.JobView{}, mapError(err)
	}
	j.ID, j.DatabaseID, j.ChangeRequestID, j.ExecutionID = security.ID(id), security.ID(database), security.ID(request), security.ID(execution)
	j.RequestedBy, j.TraceContext, j.MigrationID = security.ID(requestedBy), security.NewSecret(trace), security.ID(migrationID)
	j.Kind, j.Mode, j.Status = migration.ChangeKind(kind), migration.ExecutionMode(mode), migration.JobStatus(status)
	j.QueuedAt, j.RunAfter, j.StartedAt, j.FinishedAt = utc(j.QueuedAt), utc(j.RunAfter), optionalTime(j.StartedAt), optionalTime(j.FinishedAt)
	j.Error = jobFailure(code, message, sqlstate, retryable)
	return j, nil
}

func findJob(ctx context.Context, q querier, database, id security.ID, suffix string) (migration.JobView, error) {
	if !security.ValidID(database) || !security.ValidID(id) {
		return migration.JobView{}, security.ErrNotFound
	}
	return scanJob(q.QueryRow(ctx, "SELECT "+jobColumns+jobFrom+" WHERE j.id = $1 AND j.database_id = $2"+suffix, string(id), string(database)))
}

func (s *M2Store) FindJob(ctx context.Context, databaseID, jobID security.ID) (migration.JobView, error) {
	return findJob(ctx, s.pool, databaseID, jobID, "")
}

func (s *M2Store) JobDatabase(ctx context.Context, jobID security.ID) (security.ID, error) {
	if !security.ValidID(jobID) {
		return "", security.ErrNotFound
	}
	var database string
	if err := s.pool.QueryRow(ctx, "SELECT database_id::text FROM jobs WHERE id = $1", string(jobID)).Scan(&database); err != nil {
		return "", mapError(err)
	}
	return security.ID(database), nil
}

func (s *M2Store) ListJobs(ctx context.Context, databaseID security.ID, query migration.JobQuery) (migration.Page[migration.JobView], error) {
	page, offset, err := pageWindow(query.PageQuery)
	if err != nil || !security.ValidID(databaseID) {
		return migration.Page[migration.JobView]{}, security.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, "SELECT "+jobColumns+jobFrom+` WHERE j.database_id = $1 AND ($2 = '' OR j.status = $2)
		ORDER BY j.queued_at DESC, j.id DESC LIMIT $3 OFFSET $4`, string(databaseID), string(query.Status), page.Limit+1, offset)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.JobView, error) { return scanJob(rows) })
	if err != nil {
		return migration.Page[migration.JobView]{}, err
	}
	return finishPage(items, page), nil
}

func (s *M2Store) ListAttempts(ctx context.Context, databaseID, jobID security.ID, query migration.PageQuery) (migration.Page[migration.JobAttempt], error) {
	page, offset, err := pageWindow(query)
	if err != nil {
		return migration.Page[migration.JobAttempt]{}, security.ErrInvalidInput
	}
	if _, err := s.jobInDatabase(ctx, databaseID, jobID); err != nil {
		return migration.Page[migration.JobAttempt]{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT attempt, started_at, finished_at, status, error_code, error_message, sqlstate, retryable
		FROM job_attempts WHERE job_id = $1 AND database_id = $2 ORDER BY attempt LIMIT $3 OFFSET $4`,
		string(jobID), string(databaseID), page.Limit+1, offset)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.JobAttempt, error) {
		var a migration.JobAttempt
		var status, message string
		var code, sqlstate *string
		var retryable bool
		if err := rows.Scan(&a.Attempt, &a.StartedAt, &a.FinishedAt, &status, &code, &message, &sqlstate, &retryable); err != nil {
			return a, mapError(err)
		}
		a.StartedAt, a.FinishedAt, a.Status = utc(a.StartedAt), optionalTime(a.FinishedAt), migration.JobStatus(status)
		a.Error = jobFailure(code, message, sqlstate, retryable)
		return a, nil
	})
	if err != nil {
		return migration.Page[migration.JobAttempt]{}, err
	}
	return finishPage(items, page), nil
}

func (s *M2Store) jobInDatabase(ctx context.Context, databaseID, jobID security.ID) (security.ID, error) {
	if !security.ValidID(databaseID) || !security.ValidID(jobID) {
		return "", security.ErrNotFound
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM jobs WHERE id = $1 AND database_id = $2)", string(jobID), string(databaseID)).Scan(&exists); err != nil {
		return "", mapError(err)
	}
	if !exists {
		return "", security.ErrNotFound
	}
	return databaseID, nil
}

const executionSummaryColumns = `e.id::text, e.database_id::text, e.migration_id::text, COALESCE(e.change_request_id::text, ''),
	COALESCE(e.job_id::text, ''), e.migration_revision, e.checksum, e.operation, e.direction, COALESCE(e.reverts_execution_id::text, ''),
	COALESCE(e.forward_of_execution_id::text, ''), COALESCE(e.actor_id::text, ''), e.actor, e.started_at, e.committed_at, e.position_version`

func scanExecutionSummary(row pgx.Row, extra ...any) (migration.CommittedExecutionSummary, error) {
	var e migration.CommittedExecutionSummary
	var id, database, migrationID, request, job, operation, direction, reverts, forwardOf, actorID string
	var revision, position int64
	var checksum []byte
	targets := append([]any{&id, &database, &migrationID, &request, &job, &revision, &checksum, &operation, &direction, &reverts, &forwardOf,
		&actorID, &e.Actor, &e.StartedAt, &e.CommittedAt, &position}, extra...)
	if err := row.Scan(targets...); err != nil {
		return migration.CommittedExecutionSummary{}, mapError(err)
	}
	e.ID, e.DatabaseID, e.MigrationID, e.ChangeRequestID, e.JobID = security.ID(id), security.ID(database), security.ID(migrationID), security.ID(request), security.ID(job)
	e.MigrationRevision, e.Checksum = uint64(revision), checksumOf(checksum)
	e.Operation, e.Direction = migration.ChangeKind(operation), migration.ExecutionDirection(direction)
	e.RevertsExecutionID, e.ForwardOfExecutionID, e.ActorID = security.ID(reverts), security.ID(forwardOf), security.ID(actorID)
	e.StartedAt, e.CommittedAt, e.PositionVersion = utc(e.StartedAt), utc(e.CommittedAt), migration.PositionVersion(position)
	return e, nil
}

func findExecution(ctx context.Context, q querier, database, id security.ID) (migration.Execution, error) {
	if !security.ValidID(database) || !security.ValidID(id) {
		return migration.Execution{}, security.ErrNotFound
	}
	var title, up, reason, mode string
	var down *string
	var irreversible bool
	var batch, statements []byte
	summary, err := scanExecutionSummary(q.QueryRow(ctx, "SELECT "+executionSummaryColumns+`, e.title, e.up_sql, e.down_sql, e.irreversible,
		e.irreversible_reason, e.mode, e.batch_config, e.statements FROM executions e WHERE e.id = $1 AND e.database_id = $2`,
		string(id), string(database)), &title, &up, &down, &irreversible, &reason, &mode, &batch, &statements)
	if err != nil {
		return migration.Execution{}, err
	}
	config, err := decodeBatch(batch)
	if err != nil {
		return migration.Execution{}, err
	}
	executed, err := decodeStatements(statements)
	if err != nil {
		return migration.Execution{}, err
	}
	definition := migration.DefinitionInput{Title: title, UpSQL: security.NewSecret(up), Irreversible: irreversible,
		IrreversibleReason: reason, Mode: migration.ExecutionMode(mode), Batch: config}
	if down != nil {
		definition.DownSQL = security.NewSecret(*down)
	}
	return migration.Execution{
		ID: summary.ID, DatabaseID: summary.DatabaseID, MigrationID: summary.MigrationID, ChangeRequestID: summary.ChangeRequestID,
		JobID: summary.JobID, MigrationRevision: summary.MigrationRevision, Checksum: summary.Checksum, Operation: summary.Operation,
		Direction: summary.Direction, RevertsExecutionID: summary.RevertsExecutionID, ForwardOfExecutionID: summary.ForwardOfExecutionID,
		Definition: definition, Statements: executed, ActorID: summary.ActorID, Actor: summary.Actor, StartedAt: summary.StartedAt,
		CommittedAt: summary.CommittedAt, PositionVersion: summary.PositionVersion,
	}, nil
}

func (s *M2Store) FindExecution(ctx context.Context, databaseID, executionID security.ID) (migration.Execution, error) {
	return findExecution(ctx, s.pool, databaseID, executionID)
}

// Pasangan rollback ditentukan dengan execution ID sebelum paginasi.
func (s *M2Store) ListExecutions(ctx context.Context, databaseID security.ID, query migration.ExecutionQuery) (migration.Page[migration.CommittedExecutionSummary], error) {
	page, offset, err := pageWindow(query.PageQuery)
	if err != nil || !security.ValidID(databaseID) {
		return migration.Page[migration.CommittedExecutionSummary]{}, security.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, "SELECT "+executionSummaryColumns+` FROM executions e WHERE e.database_id = $1 AND (NOT $2 OR (
			e.reverts_execution_id IS NULL AND
			NOT EXISTS (SELECT 1 FROM executions r WHERE r.database_id = e.database_id AND r.reverts_execution_id = e.id)
		)) ORDER BY e.committed_at DESC, e.id DESC LIMIT $3 OFFSET $4`, string(databaseID), query.HideRollback, page.Limit+1, offset)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.CommittedExecutionSummary, error) { return scanExecutionSummary(rows) })
	if err != nil {
		return migration.Page[migration.CommittedExecutionSummary]{}, err
	}
	return finishPage(items, page), nil
}

func (s *M2Store) ListExecutedStatements(ctx context.Context, databaseID, executionID security.ID, query migration.PageQuery) (migration.Page[migration.ExecutedStatement], error) {
	page, offset, err := pageWindow(query)
	if err != nil {
		return migration.Page[migration.ExecutedStatement]{}, security.ErrInvalidInput
	}
	if !security.ValidID(databaseID) || !security.ValidID(executionID) {
		return migration.Page[migration.ExecutedStatement]{}, security.ErrNotFound
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM executions WHERE id = $1 AND database_id = $2)", string(executionID), string(databaseID)).Scan(&exists); err != nil {
		return migration.Page[migration.ExecutedStatement]{}, mapError(err)
	}
	if !exists {
		return migration.Page[migration.ExecutedStatement]{}, security.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `WITH batch AS (
		SELECT DISTINCT ON (b.batch_no) b.batch_no, b.statement AS value
		FROM executions e JOIN jobs j ON j.execution_id = e.id AND j.database_id = e.database_id
		JOIN batch_statements b ON b.job_id = j.id
		WHERE e.id = $1 AND e.database_id = $2 ORDER BY b.batch_no, j.queued_at DESC, j.id DESC
	), records AS (
		SELECT batch_no AS ordinal, value FROM batch
		UNION ALL
		SELECT s.n AS ordinal, s.value FROM executions e,
			jsonb_array_elements(e.statements) WITH ORDINALITY AS s(value, n)
		WHERE e.id = $1 AND e.database_id = $2 AND NOT EXISTS (SELECT 1 FROM batch)
	)
	SELECT value FROM records ORDER BY ordinal LIMIT $3 OFFSET $4`, string(executionID), string(databaseID), page.Limit+1, offset)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.ExecutedStatement, error) {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return migration.ExecutedStatement{}, mapError(err)
		}
		return decodeStatement(raw)
	})
	if err != nil {
		return migration.Page[migration.ExecutedStatement]{}, err
	}
	return finishPage(items, page), nil
}

// buildPosition memutar ulang history: up masuk stack aktif, down mengeluarkan execution yang
// dibatalkan dan memindahkan up aslinya ke redo chain.
func buildPosition(history []migration.CommittedExecutionSummary) (active, forward []migration.CommittedExecutionSummary, err error) {
	byID := make(map[security.ID]migration.CommittedExecutionSummary, len(history))
	var redo []migration.CommittedExecutionSummary
	for _, execution := range history {
		byID[execution.ID] = execution
		switch {
		case execution.Direction == migration.DirectionUp:
			if execution.Operation == migration.ChangeForward {
				for i := len(redo) - 1; i >= 0; i-- {
					if redo[i].ID == execution.ForwardOfExecutionID {
						redo = append(redo[:i], redo[i+1:]...)
						break
					}
				}
			}
			active = append(active, execution)
		case execution.RevertsExecutionID != "":
			index := -1
			for i := len(active) - 1; i >= 0; i-- {
				if active[i].ID == execution.RevertsExecutionID {
					index = i
					break
				}
			}
			if index < 0 {
				return nil, nil, security.ErrUnavailable
			}
			reverted := active[index]
			active = append(active[:index], active[index+1:]...)
			original := reverted
			if reverted.ForwardOfExecutionID != "" {
				if found, ok := byID[reverted.ForwardOfExecutionID]; ok {
					original = found
				}
			}
			redo = append(redo, original)
		}
	}
	forward = make([]migration.CommittedExecutionSummary, 0, len(redo))
	for i := len(redo) - 1; i >= 0; i-- {
		forward = append(forward, redo[i])
	}
	if active == nil {
		active = []migration.CommittedExecutionSummary{}
	}
	return active, forward, nil
}

func readPosition(ctx context.Context, q querier, databaseID security.ID) (migration.Position, error) {
	if !security.ValidID(databaseID) {
		return migration.Position{}, security.ErrNotFound
	}
	var version int64
	var blocked string
	if err := q.QueryRow(ctx, "SELECT position_version, COALESCE(blocked_by_job_id::text, '') FROM database_workflows WHERE database_id = $1",
		string(databaseID)).Scan(&version, &blocked); err != nil {
		return migration.Position{}, mapError(err)
	}
	rows, err := q.Query(ctx, "SELECT "+executionSummaryColumns+" FROM executions e WHERE e.database_id = $1 ORDER BY e.position_version",
		string(databaseID))
	history, err := collect(rows, err, func(rows pgx.Rows) (migration.CommittedExecutionSummary, error) { return scanExecutionSummary(rows) })
	if err != nil {
		return migration.Position{}, err
	}
	active, forward, err := buildPosition(history)
	if err != nil {
		return migration.Position{}, err
	}
	return migration.Position{Version: migration.PositionVersion(version), Active: active, Forward: forward, BlockedByJobID: security.ID(blocked)}, nil
}

func (s *M2Store) ReadPosition(ctx context.Context, databaseID security.ID) (migration.Position, error) {
	var position migration.Position
	err := withTx(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		position, err = readPosition(ctx, tx, databaseID)
		return err
	})
	return position, err
}

// Group tanpa leader aktif dikembalikan dengan LeaderUserID kosong.
func requiredApprovers(ctx context.Context, q querier, databaseID security.ID) ([]migration.RequiredApproval, error) {
	if !security.ValidID(databaseID) {
		return nil, security.ErrInvalidInput
	}
	rows, err := q.Query(ctx, `SELECT g.id::text, g.name, x.access, CASE WHEN u.active IS TRUE THEN u.id::text ELSE '' END
		FROM group_database_grants x JOIN groups g ON g.id = x.group_id LEFT JOIN users u ON u.id = g.leader_user_id
		WHERE x.database_id = $1 ORDER BY g.name, g.id`, string(databaseID))
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.RequiredApproval, error) {
		var group, name, access, leader string
		if err := rows.Scan(&group, &name, &access, &leader); err != nil {
			return migration.RequiredApproval{}, mapError(err)
		}
		return migration.RequiredApproval{GroupID: security.ID(group), GroupName: name, Access: security.DatabaseAccess(access), LeaderUserID: security.ID(leader)}, nil
	})
	if items == nil && err == nil {
		items = []migration.RequiredApproval{}
	}
	return items, err
}

func (s *M2Store) RequiredApprovers(ctx context.Context, databaseID security.ID) ([]migration.RequiredApproval, error) {
	return requiredApprovers(ctx, s.pool, databaseID)
}

func isLeader(ctx context.Context, q querier, userID, groupID security.ID) (bool, error) {
	if !security.ValidID(userID) || !security.ValidID(groupID) {
		return false, nil
	}
	var leader bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM groups g JOIN users u ON u.id = g.leader_user_id
		WHERE g.id = $1 AND g.leader_user_id = $2 AND u.active)`, string(groupID), string(userID)).Scan(&leader)
	return leader, mapError(err)
}

func (s *M2Store) IsLeader(ctx context.Context, userID, groupID security.ID) (bool, error) {
	return isLeader(ctx, s.pool, userID, groupID)
}

func (s *M2Store) LeaderGroups(ctx context.Context, userID security.ID) ([]security.ID, error) {
	if !security.ValidID(userID) {
		return []security.ID{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT g.id::text FROM groups g JOIN users u ON u.id = g.leader_user_id
		WHERE g.leader_user_id = $1 AND u.active ORDER BY g.id`, string(userID))
	items, err := collect(rows, err, func(rows pgx.Rows) (security.ID, error) {
		var id string
		err := rows.Scan(&id)
		return security.ID(id), mapError(err)
	})
	if items == nil && err == nil {
		items = []security.ID{}
	}
	return items, err
}
