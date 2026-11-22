package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const checksumHex = "\\x" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type m2Fixture struct {
	pool               *pgxpool.Pool
	user, leader       security.ID
	group, database    security.ID
	migration, cr, job security.ID
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func expectState(t *testing.T, err error, want string) {
	t.Helper()
	if got := sqlState(err); got != want {
		t.Fatalf("SQLSTATE = %q (%v), want %q", got, err, want)
	}
}

func insertDatabase(t *testing.T, pool *pgxpool.Pool, name string) security.ID {
	t.Helper()
	id := newID(t)
	exec(t, pool, "INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, $2, '\\x00')", string(id), name)
	return id
}

func newM2Fixture(t *testing.T) m2Fixture {
	t.Helper()
	pool := testPlatform(t)
	f := m2Fixture{pool: pool}
	f.user = insertUser(t, pool, "requester", false)
	f.leader = insertUser(t, pool, "leader", false)
	f.group = newID(t)
	exec(t, pool, "INSERT INTO groups (id, name) VALUES ($1, 'team')", string(f.group))
	exec(t, pool, "INSERT INTO group_memberships (group_id, user_id) VALUES ($1, $2), ($1, $3)", string(f.group), string(f.leader), string(f.user))
	exec(t, pool, "UPDATE groups SET leader_user_id = $2 WHERE id = $1", string(f.group), string(f.leader))
	f.database = insertDatabase(t, pool, "target")
	exec(t, pool, "INSERT INTO group_database_grants (group_id, database_id, access) VALUES ($1, $2, 'view_edit')", string(f.group), string(f.database))
	f.migration = newID(t)
	exec(t, pool, `INSERT INTO migrations (id, database_id, title, sql, status, revision, created_by, created_at, updated_at,
		down_sql, irreversible, irreversible_reason, definition_checksum)
		VALUES ($1, $2, 'add column', 'ALTER TABLE t ADD c int', 'draft', 1, $3, now(), now(),
		'ALTER TABLE t DROP c', false, '', '`+checksumHex+`')`, string(f.migration), string(f.database), string(f.user))
	f.cr = f.insertCR(t, "pending_review")
	return f
}

func (f m2Fixture) insertCR(t *testing.T, status string) security.ID {
	t.Helper()
	id := newID(t)
	exec(t, f.pool, `INSERT INTO change_requests (id, database_id, migration_id, kind, status, migration_revision, checksum,
		expected_position_version, requested_by, created_at, updated_at)
		VALUES ($1, $2, $3, 'apply', $4, 1, '`+checksumHex+`', 0, $5, now(), now())`,
		string(id), string(f.database), string(f.migration), status, string(f.user))
	return id
}

func (f *m2Fixture) insertJob(t *testing.T) {
	t.Helper()
	f.job = newID(t)
	exec(t, f.pool, `INSERT INTO jobs (id, database_id, change_request_id, execution_id, requested_by, status, max_attempts, run_after, queued_at)
		VALUES ($1, $2, $3, $4, $5, 'queued', 3, now(), now())`, string(f.job), string(f.database), string(f.cr), string(newID(t)), string(f.user))
}

func (f m2Fixture) insertExecution(t *testing.T, id security.ID, operation, direction string, reverts, forwardOf *string, version int) error {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `INSERT INTO executions (id, database_id, migration_id, change_request_id, job_id,
		migration_revision, checksum, operation, direction, reverts_execution_id, forward_of_execution_id, title, up_sql, down_sql,
		irreversible, irreversible_reason, mode, actor_id, actor, started_at, committed_at, position_version)
		VALUES ($1, $2, $3, $4, $5, 1, '`+checksumHex+`', $6, $7, $8, $9, 'add column', 'ALTER TABLE t ADD c int', 'ALTER TABLE t DROP c',
		false, '', 'normal', $10, 'requester', now(), now(), $11)`,
		string(id), string(f.database), string(f.migration), string(f.cr), string(f.job), operation, direction, reverts, forwardOf, string(f.user), version)
	return err
}

func TestM2SchemaMigrateIsIdempotent(t *testing.T) {
	pool := testPlatform(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	for _, table := range []string{"database_workflows", "change_requests", "change_request_approvals", "jobs", "job_attempts", "job_batches", "executions"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil || !exists {
			t.Fatalf("table %s missing (%v)", table, err)
		}
	}
}

func TestM2SchemaBackfillsMilestoneOneHistory(t *testing.T) {
	dsn := createDatabase(t)
	ctx := context.Background()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// Simulasi platform M1: hanya 0001 dan 0002 yang diterapkan.
	exec(t, pool, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`)
	for _, name := range []string{"schema/0001_platform.sql", "schema/0002_managed_database_engine.sql"} {
		body, err := schemaFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		exec(t, pool, string(body))
		exec(t, pool, "INSERT INTO schema_migrations (name) VALUES ($1)", name)
	}
	user := insertUser(t, pool, "alice", false)
	database := insertDatabase(t, pool, "legacy")
	executed := time.Now().UTC().Truncate(time.Second)
	applied := []security.ID{newID(t), newID(t)}
	for i, id := range applied {
		exec(t, pool, `INSERT INTO migrations (id, database_id, title, sql, status, version, revision, execution_id, checksum,
			actor_id, actor, executed_at, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'applied', $5, 1, $6, '`+checksumHex+`', $7, 'alice', $8, $7, $8, $8)`,
			string(id), string(database), "m"+string(rune('a'+i)), "SELECT "+string(rune('1'+i)), i+1, string(newID(t)), string(user), executed.Add(time.Duration(i)*time.Minute))
	}
	draft := newID(t)
	exec(t, pool, `INSERT INTO migrations (id, database_id, title, sql, status, revision, created_by, created_at, updated_at)
		VALUES ($1, $2, 'draft', 'SELECT 3', 'draft', 1, $3, now(), now())`, string(draft), string(database), string(user))

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate over M1 data: %v", err)
	}

	var count, version int
	if err := pool.QueryRow(ctx, "SELECT count(*), max(position_version) FROM executions WHERE database_id = $1 AND backfilled AND operation = 'apply'", string(database)).Scan(&count, &version); err != nil || count != 2 || version != 2 {
		t.Fatalf("backfilled history = %d rows, version %d (%v), want 2/2", count, version, err)
	}
	var first string
	if err := pool.QueryRow(ctx, "SELECT migration_id::text FROM executions WHERE database_id = $1 AND position_version = 1", string(database)).Scan(&first); err != nil || first != string(applied[0]) {
		t.Fatalf("position 1 = %s (%v), want first M1 version", first, err)
	}
	if err := pool.QueryRow(ctx, "SELECT position_version FROM database_workflows WHERE database_id = $1", string(database)).Scan(&version); err != nil || version != 2 {
		t.Fatalf("workflow position_version = %d (%v), want 2", version, err)
	}
	var frozen int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM migrations WHERE frozen_at IS NOT NULL AND irreversible").Scan(&frozen); err != nil || frozen != 2 {
		t.Fatalf("frozen legacy rows = %d (%v), want 2", frozen, err)
	}
	exec(t, pool, "UPDATE migrations SET title = 'edited', sql = 'SELECT 4', revision = revision + 1 WHERE id = $1", string(draft))
	_, err = pool.Exec(ctx, "UPDATE migrations SET sql = 'DROP TABLE x' WHERE id = $1", string(applied[0]))
	expectState(t, err, "23514")
}

func TestM2SchemaDefinitionRules(t *testing.T) {
	f := newM2Fixture(t)
	ctx := context.Background()

	_, err := f.pool.Exec(ctx, "UPDATE migrations SET down_sql = NULL WHERE id = $1", string(f.migration))
	expectState(t, err, "23514")
	_, err = f.pool.Exec(ctx, "UPDATE migrations SET irreversible = true, irreversible_reason = '' , down_sql = NULL WHERE id = $1", string(f.migration))
	expectState(t, err, "23514")
	_, err = f.pool.Exec(ctx, `UPDATE migrations SET mode = 'batch', batch_config = '{"schema":"public","table":"t","key_column":"id","batch_size":50,"scope_where":null}' WHERE id = $1`, string(f.migration))
	expectState(t, err, "23514")
	exec(t, f.pool, `UPDATE migrations SET mode = 'batch', batch_config = '{"schema":"public","table":"t","key_column":"id","batch_size":1000,"scope_where":null}' WHERE id = $1`, string(f.migration))

	exec(t, f.pool, "UPDATE migrations SET frozen_at = now() WHERE id = $1", string(f.migration))
	for _, statement := range []string{
		"UPDATE migrations SET sql = 'DROP TABLE t' WHERE id = $1",
		"UPDATE migrations SET down_sql = 'SELECT 1' WHERE id = $1",
		"UPDATE migrations SET revision = revision + 1 WHERE id = $1",
		"UPDATE migrations SET frozen_at = NULL WHERE id = $1",
		"UPDATE migrations SET status = 'pending', version = 1, execution_id = gen_random_uuid() WHERE id = $1",
		"DELETE FROM migrations WHERE id = $1",
	} {
		_, err := f.pool.Exec(ctx, statement, string(f.migration))
		expectState(t, err, "23514")
	}
	exec(t, f.pool, "UPDATE migrations SET first_execution_at = now() WHERE id = $1", string(f.migration))
	_, err = f.pool.Exec(ctx, "UPDATE migrations SET first_execution_at = now() + interval '1 hour' WHERE id = $1", string(f.migration))
	expectState(t, err, "23514")
}

func TestM2SchemaGroupLeaderMustBeMember(t *testing.T) {
	f := newM2Fixture(t)
	ctx := context.Background()
	outsider := insertUser(t, f.pool, "outsider", false)
	_, err := f.pool.Exec(ctx, "UPDATE groups SET leader_user_id = $2 WHERE id = $1", string(f.group), string(outsider))
	expectState(t, err, "23503")
	_, err = f.pool.Exec(ctx, "DELETE FROM group_memberships WHERE group_id = $1 AND user_id = $2", string(f.group), string(f.leader))
	expectState(t, err, "23503")
	exec(t, f.pool, "DELETE FROM group_memberships WHERE group_id = $1 AND user_id = $2", string(f.group), string(f.user))

	other := newID(t)
	exec(t, f.pool, "INSERT INTO groups (id, name) VALUES ($1, 'other')", string(other))
	exec(t, f.pool, "INSERT INTO group_memberships (group_id, user_id) VALUES ($1, $2)", string(other), string(outsider))
	exec(t, f.pool, "UPDATE groups SET leader_user_id = $2 WHERE id = $1", string(other), string(outsider))
	exec(t, f.pool, "DELETE FROM groups WHERE id = $1", string(other))
}

func TestM2SchemaWorkflowGuardIsCreatedPerDatabase(t *testing.T) {
	f := newM2Fixture(t)
	var version int
	if err := f.pool.QueryRow(context.Background(), "SELECT position_version FROM database_workflows WHERE database_id = $1", string(f.database)).Scan(&version); err != nil || version != 0 {
		t.Fatalf("workflow guard version = %d (%v), want 0", version, err)
	}
}

func TestM2SchemaChangeRequestsAndApprovals(t *testing.T) {
	f := newM2Fixture(t)
	ctx := context.Background()

	_, err := f.pool.Exec(ctx, `INSERT INTO change_requests (id, database_id, migration_id, kind, status, migration_revision, checksum,
		expected_position_version, requested_by, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, 'apply', 'approved', 1, '`+checksumHex+`', 0, $3, now(), now())`,
		string(f.database), string(f.migration), string(f.user))
	expectState(t, err, "23505")
	_, err = f.pool.Exec(ctx, `INSERT INTO change_requests (id, database_id, migration_id, kind, status, migration_revision, checksum,
		expected_position_version, requested_by, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, 'rollback', 'rejected', 1, '`+checksumHex+`', 0, $3, now(), now())`,
		string(f.database), string(f.migration), string(f.user))
	expectState(t, err, "23514")
	otherDB := insertDatabase(t, f.pool, "other")
	_, err = f.pool.Exec(ctx, `INSERT INTO change_requests (id, database_id, migration_id, kind, status, migration_revision, checksum,
		expected_position_version, requested_by, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, 'apply', 'pending_review', 1, '`+checksumHex+`', 0, $3, now(), now())`,
		string(otherDB), string(f.migration), string(f.user))
	expectState(t, err, "23503")

	exec(t, f.pool, `INSERT INTO change_request_approvals (change_request_id, group_id, group_name, leader_user_id, access)
		VALUES ($1, $2, 'team', $3, 'view_edit')`, string(f.cr), string(f.group), string(f.leader))
	_, err = f.pool.Exec(ctx, "UPDATE change_request_approvals SET leader_user_id = $2 WHERE change_request_id = $1", string(f.cr), string(f.user))
	expectState(t, err, "23514")
	exec(t, f.pool, `UPDATE change_request_approvals SET decision = 'approve', decided_by = $2, decided_at = now(), note = 'ok'
		WHERE change_request_id = $1`, string(f.cr), string(f.leader))
	_, err = f.pool.Exec(ctx, "UPDATE change_request_approvals SET decision = 'reject' WHERE change_request_id = $1", string(f.cr))
	expectState(t, err, "23514")
	_, err = f.pool.Exec(ctx, "DELETE FROM change_request_approvals WHERE change_request_id = $1", string(f.cr))
	expectState(t, err, "23514")

	exec(t, f.pool, "UPDATE change_requests SET status = 'rejected' WHERE id = $1", string(f.cr))
	f.insertCR(t, "pending_review")
}

func TestM2SchemaJobsAndAttempts(t *testing.T) {
	f := newM2Fixture(t)
	ctx := context.Background()
	f.insertJob(t)

	_, err := f.pool.Exec(ctx, `INSERT INTO jobs (id, database_id, change_request_id, execution_id, requested_by, status, max_attempts, run_after, queued_at)
		VALUES (gen_random_uuid(), $1, $2, gen_random_uuid(), $3, 'queued', 3, now(), now())`, string(f.database), string(f.cr), string(f.user))
	expectState(t, err, "23505")
	_, err = f.pool.Exec(ctx, "UPDATE jobs SET status = 'succeeded' WHERE id = $1", string(f.job))
	expectState(t, err, "23514")
	_, err = f.pool.Exec(ctx, "UPDATE jobs SET lease_owner = 'w1' WHERE id = $1", string(f.job))
	expectState(t, err, "23514")

	attempt := func(n int) error {
		_, err := f.pool.Exec(ctx, `INSERT INTO job_attempts (job_id, attempt, database_id, change_request_id, migration_id, migration_revision,
			execution_id, kind, actor_id, actor, worker, lease_token, status, started_at)
			SELECT id, $2::integer, database_id, change_request_id, $3, 1, execution_id, 'apply', $4, 'requester', 'w1', $2::bigint, 'running', now()
			FROM jobs WHERE id = $1`, string(f.job), n, string(f.migration), string(f.user))
		return err
	}
	if err := attempt(1); err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, "UPDATE job_attempts SET status = 'failed', finished_at = now() WHERE job_id = $1", string(f.job))
	expectState(t, err, "23514")
	exec(t, f.pool, `UPDATE job_attempts SET status = 'failed', error_code = 'sql_error', sqlstate = '42P01', finished_at = now(),
		statements = '[{"query":"ALTER TABLE t ADD c int","params":[],"batch":null}]' WHERE job_id = $1`, string(f.job))
	_, err = f.pool.Exec(ctx, "UPDATE job_attempts SET error_message = 'changed' WHERE job_id = $1", string(f.job))
	expectState(t, err, "23514")
	_, err = f.pool.Exec(ctx, "DELETE FROM job_attempts WHERE job_id = $1", string(f.job))
	expectState(t, err, "23514")
	_, err = f.pool.Exec(ctx, "TRUNCATE job_attempts CASCADE")
	expectState(t, err, "23514")
	if err := attempt(2); err != nil {
		t.Fatal(err)
	}
}

func TestM2SchemaBatchMirror(t *testing.T) {
	f := newM2Fixture(t)
	ctx := context.Background()
	f.insertJob(t)
	execution := newID(t)
	exec(t, f.pool, `INSERT INTO job_batches (execution_id, batch_no, job_id, cursor_from, cursor_to, scope_max, rows, committed_at)
		VALUES ($1, 1, $2, NULL, '1000', '5000', 1000, now())`, string(execution), string(f.job))
	_, err := f.pool.Exec(ctx, `INSERT INTO job_batches (execution_id, batch_no, job_id, cursor_from, cursor_to, scope_max, rows, committed_at)
		VALUES ($1, 1, $2, '1000', '2000', '5000', 1000, now())`, string(execution), string(f.job))
	expectState(t, err, "23505")
	_, err = f.pool.Exec(ctx, "UPDATE job_batches SET rows = 1 WHERE execution_id = $1", string(execution))
	expectState(t, err, "23514")
	exec(t, f.pool, "UPDATE job_batches SET reverted_at = now(), reverted_by_job_id = $2 WHERE execution_id = $1", string(execution), string(f.job))
	_, err = f.pool.Exec(ctx, "UPDATE job_batches SET reverted_at = now() WHERE execution_id = $1", string(execution))
	expectState(t, err, "23514")
}

func TestM2SchemaExecutionsAreAppendOnly(t *testing.T) {
	f := newM2Fixture(t)
	ctx := context.Background()
	f.insertJob(t)
	up, down := newID(t), newID(t)
	if err := f.insertExecution(t, up, "apply", "up", nil, nil, 1); err != nil {
		t.Fatal(err)
	}
	upText := string(up)
	if err := f.insertExecution(t, newID(t), "rollback", "down", nil, nil, 2); sqlState(err) != "23514" {
		t.Fatalf("rollback without reverts_execution_id: %v", err)
	}
	if err := f.insertExecution(t, down, "rollback", "down", &upText, nil, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.insertExecution(t, newID(t), "rollback", "down", &upText, nil, 3); sqlState(err) != "23505" {
		t.Fatalf("second rollback of the same execution: %v", err)
	}
	if err := f.insertExecution(t, newID(t), "forward", "up", nil, &upText, 2); sqlState(err) != "23505" {
		t.Fatalf("duplicate position version: %v", err)
	}
	if err := f.insertExecution(t, newID(t), "forward", "up", nil, &upText, 3); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE executions SET actor = 'mallory' WHERE id = $1",
		"DELETE FROM executions WHERE id = $1",
	} {
		_, err := f.pool.Exec(ctx, statement, string(up))
		expectState(t, err, "23514")
	}
	_, err := f.pool.Exec(ctx, "TRUNCATE executions CASCADE")
	expectState(t, err, "23514")
	_, err = f.pool.Exec(ctx, `INSERT INTO executions (id, database_id, migration_id, migration_revision, operation, direction, title, up_sql,
		irreversible, irreversible_reason, mode, actor, started_at, committed_at, position_version)
		VALUES (gen_random_uuid(), $1, $2, 1, 'apply', 'up', 't', 'SELECT 1', true, 'x', 'normal', 'a', now(), now(), 9)`,
		string(f.database), string(f.migration))
	if sqlState(err) != "23514" || !strings.Contains(err.Error(), "executions") {
		t.Fatalf("history without job: %v", err)
	}
}
