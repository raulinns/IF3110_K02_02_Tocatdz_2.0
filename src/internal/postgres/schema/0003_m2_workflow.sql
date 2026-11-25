-- Milestone 2: definisi up/down, change request, approval, queue, attempt, dan history.
-- Kolom M1 `sql` dipakai sebagai up SQL; status M1 `pending` dipetakan ke `uncertain` oleh store.

-- FK komposit: leader wajib anggota group.
ALTER TABLE groups ADD COLUMN leader_user_id uuid;
ALTER TABLE groups ADD CONSTRAINT groups_leader_is_member
	FOREIGN KEY (id, leader_user_id) REFERENCES group_memberships (group_id, user_id);

-- Default irreversible hanya agar INSERT draft M1 tetap lolos CHECK; hapus setelah UI M1 dicabut.
ALTER TABLE migrations
	ADD COLUMN down_sql text,
	ADD COLUMN irreversible boolean NOT NULL DEFAULT true,
	ADD COLUMN irreversible_reason text NOT NULL DEFAULT 'Milestone 1 migration has no down SQL',
	ADD COLUMN mode text NOT NULL DEFAULT 'normal',
	ADD COLUMN batch_config jsonb,
	ADD COLUMN definition_checksum bytea,
	ADD COLUMN frozen_at timestamptz,
	ADD COLUMN first_execution_at timestamptz;

ALTER TABLE migrations
	ADD CONSTRAINT migrations_database_id_key UNIQUE (database_id, id),
	ADD CONSTRAINT migrations_mode_check CHECK (mode IN ('normal', 'batch')),
	ADD CONSTRAINT migrations_reversibility_check CHECK (
		(irreversible AND down_sql IS NULL AND btrim(irreversible_reason) <> '') OR
		(NOT irreversible AND down_sql IS NOT NULL AND btrim(down_sql) <> '' AND irreversible_reason = '')
	),
	ADD CONSTRAINT migrations_reason_length_check CHECK (char_length(irreversible_reason) <= 2000),
	ADD CONSTRAINT migrations_batch_config_check CHECK (
		(mode = 'normal' AND batch_config IS NULL) OR (
			mode = 'batch' AND jsonb_typeof(batch_config) = 'object' AND
			jsonb_typeof(batch_config -> 'schema') = 'string' AND
			jsonb_typeof(batch_config -> 'table') = 'string' AND
			jsonb_typeof(batch_config -> 'key_column') = 'string' AND
			jsonb_typeof(batch_config -> 'batch_size') = 'number' AND
			jsonb_typeof(batch_config -> 'scope_where') IN ('string', 'null') AND
			(batch_config ->> 'batch_size')::numeric BETWEEN 100 AND 100000 AND
			(batch_config ->> 'batch_size')::numeric = trunc((batch_config ->> 'batch_size')::numeric)
		)
	),
	ADD CONSTRAINT migrations_definition_checksum_check
		CHECK (definition_checksum IS NULL OR octet_length(definition_checksum) = 32),
	ADD CONSTRAINT migrations_frozen_checksum_check
		CHECK (frozen_at IS NULL OR status <> 'draft' OR definition_checksum IS NOT NULL),
	ADD CONSTRAINT migrations_first_execution_frozen_check
		CHECK (first_execution_at IS NULL OR frozen_at IS NOT NULL);

-- Definisi yang sudah dibekukan tidak boleh berubah, termasuk lewat jalur eksekusi M1.
CREATE FUNCTION migrations_protect_definition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF TG_OP = 'DELETE' THEN
		IF OLD.frozen_at IS NOT NULL THEN
			RAISE EXCEPTION 'frozen migration definition is immutable' USING ERRCODE = 'check_violation';
		END IF;
		RETURN OLD;
	END IF;
	IF OLD.frozen_at IS NULL THEN
		RETURN NEW;
	END IF;
	IF NEW.frozen_at IS DISTINCT FROM OLD.frozen_at OR
		NEW.database_id IS DISTINCT FROM OLD.database_id OR
		NEW.title IS DISTINCT FROM OLD.title OR
		NEW.sql IS DISTINCT FROM OLD.sql OR
		NEW.down_sql IS DISTINCT FROM OLD.down_sql OR
		NEW.irreversible IS DISTINCT FROM OLD.irreversible OR
		NEW.irreversible_reason IS DISTINCT FROM OLD.irreversible_reason OR
		NEW.mode IS DISTINCT FROM OLD.mode OR
		NEW.batch_config IS DISTINCT FROM OLD.batch_config OR
		NEW.definition_checksum IS DISTINCT FROM OLD.definition_checksum OR
		(OLD.status = 'draft' AND NEW.revision IS DISTINCT FROM OLD.revision) OR
		(OLD.first_execution_at IS NOT NULL AND NEW.first_execution_at IS DISTINCT FROM OLD.first_execution_at) OR
		(NEW.status IS DISTINCT FROM OLD.status AND OLD.status <> 'pending')
	THEN
		RAISE EXCEPTION 'frozen migration definition is immutable' USING ERRCODE = 'check_violation';
	END IF;
	RETURN NEW;
END;
$$;

-- Migration M1 yang pernah dieksekusi ikut dibekukan; trigger M1 dimatikan sementara untuk backfill ini.
ALTER TABLE migrations DISABLE TRIGGER migrations_protect_history;
UPDATE migrations
SET frozen_at = COALESCE(executed_at, updated_at),
	first_execution_at = COALESCE(executed_at, updated_at)
WHERE status <> 'draft';
ALTER TABLE migrations ENABLE TRIGGER migrations_protect_history;

CREATE TRIGGER migrations_protect_definition
	BEFORE UPDATE OR DELETE ON migrations
	FOR EACH ROW EXECUTE FUNCTION migrations_protect_definition();

-- Baris guard per database: dikunci oleh workflow dan admin, menyimpan versi posisi dan barrier.
CREATE TABLE database_workflows (
	database_id uuid PRIMARY KEY REFERENCES managed_databases (id) ON DELETE CASCADE,
	position_version bigint NOT NULL DEFAULT 0 CHECK (position_version >= 0),
	blocked_by_job_id uuid,
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION managed_databases_create_workflow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	INSERT INTO database_workflows (database_id) VALUES (NEW.id) ON CONFLICT DO NOTHING;
	RETURN NEW;
END;
$$;

CREATE TRIGGER managed_databases_create_workflow
	AFTER INSERT ON managed_databases
	FOR EACH ROW EXECUTE FUNCTION managed_databases_create_workflow();

CREATE TABLE change_requests (
	id uuid PRIMARY KEY,
	database_id uuid NOT NULL REFERENCES managed_databases (id),
	migration_id uuid NOT NULL,
	kind text NOT NULL CHECK (kind IN ('apply', 'rollback', 'forward', 'resume', 'revert_batches')),
	status text NOT NULL CHECK (status IN ('pending_review', 'approved', 'rejected', 'queued', 'running',
		'succeeded', 'failed', 'partial', 'uncertain', 'partially_reverted')),
	revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
	migration_revision bigint NOT NULL CHECK (migration_revision > 0),
	checksum bytea NOT NULL CHECK (octet_length(checksum) = 32),
	target_execution_id uuid,
	source_job_id uuid,
	expected_position_version bigint NOT NULL CHECK (expected_position_version >= 0),
	requested_by uuid NOT NULL REFERENCES users (id),
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL,
	UNIQUE (database_id, id),
	FOREIGN KEY (database_id, migration_id) REFERENCES migrations (database_id, id),
	CHECK ((kind IN ('rollback', 'forward')) = (target_execution_id IS NOT NULL)),
	CHECK (kind NOT IN ('resume', 'revert_batches') OR source_job_id IS NOT NULL),
	CHECK (kind IN ('apply', 'resume', 'revert_batches') OR source_job_id IS NULL)
);

-- Satu CR terbuka per database. Partial dicatat sebagai barrier agar CR pemulihan tetap bisa dibuat.
CREATE UNIQUE INDEX change_requests_one_open_idx ON change_requests (database_id)
	WHERE status IN ('pending_review', 'approved', 'queued', 'running', 'uncertain');
CREATE INDEX change_requests_list_idx ON change_requests (database_id, created_at DESC, id DESC);
CREATE INDEX change_requests_status_idx ON change_requests (database_id, status, created_at DESC, id DESC);
CREATE INDEX change_requests_migration_idx ON change_requests (migration_id);

-- Snapshot approver saat submit; tanpa FK ke groups agar tetap utuh walau group dihapus.
CREATE TABLE change_request_approvals (
	change_request_id uuid NOT NULL REFERENCES change_requests (id),
	group_id uuid NOT NULL,
	group_name text NOT NULL,
	leader_user_id uuid NOT NULL REFERENCES users (id),
	access text NOT NULL CHECK (access IN ('view_only', 'view_edit')),
	decision text CHECK (decision IN ('approve', 'reject')),
	decided_by uuid REFERENCES users (id),
	note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
	decided_at timestamptz,
	PRIMARY KEY (change_request_id, group_id),
	CHECK ((decision IS NULL) = (decided_by IS NULL)),
	CHECK ((decision IS NULL) = (decided_at IS NULL)),
	CHECK (decision IS NOT NULL OR note = '')
);

CREATE INDEX change_request_approvals_leader_idx ON change_request_approvals (leader_user_id) WHERE decision IS NULL;

CREATE FUNCTION change_request_approvals_protect() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF TG_OP = 'DELETE' THEN
		RAISE EXCEPTION 'approval record is immutable' USING ERRCODE = 'check_violation';
	END IF;
	IF NEW.change_request_id IS DISTINCT FROM OLD.change_request_id OR
		NEW.group_id IS DISTINCT FROM OLD.group_id OR
		NEW.group_name IS DISTINCT FROM OLD.group_name OR
		NEW.leader_user_id IS DISTINCT FROM OLD.leader_user_id OR
		NEW.access IS DISTINCT FROM OLD.access OR
		OLD.decision IS NOT NULL
	THEN
		RAISE EXCEPTION 'approval record is immutable' USING ERRCODE = 'check_violation';
	END IF;
	RETURN NEW;
END;
$$;

CREATE TRIGGER change_request_approvals_protect
	BEFORE UPDATE OR DELETE ON change_request_approvals
	FOR EACH ROW EXECUTE FUNCTION change_request_approvals_protect();

-- Queue: satu job per change request.
CREATE TABLE jobs (
	id uuid PRIMARY KEY,
	database_id uuid NOT NULL,
	change_request_id uuid NOT NULL UNIQUE,
	execution_id uuid NOT NULL,
	source_job_id uuid REFERENCES jobs (id),
	requested_by uuid NOT NULL REFERENCES users (id),
	status text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'partial',
		'uncertain', 'partially_reverted')),
	attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
	max_attempts integer NOT NULL CHECK (max_attempts BETWEEN 1 AND 10),
	run_after timestamptz NOT NULL,
	lease_owner text CHECK (lease_owner IS NULL OR char_length(lease_owner) BETWEEN 1 AND 200),
	lease_token bigint NOT NULL DEFAULT 0 CHECK (lease_token >= 0),
	lease_until timestamptz,
	request_id text NOT NULL DEFAULT '' CHECK (char_length(request_id) <= 128),
	trace_context text NOT NULL DEFAULT '' CHECK (char_length(trace_context) <= 4096),
	execution_plan jsonb,
	scope_max text CHECK (scope_max IS NULL OR scope_max ~ '^-?[0-9]{1,19}$'),
	partial boolean NOT NULL DEFAULT false,
	batches_committed bigint NOT NULL DEFAULT 0 CHECK (batches_committed >= 0),
	rows_committed bigint NOT NULL DEFAULT 0 CHECK (rows_committed >= 0),
	batches_reverted bigint NOT NULL DEFAULT 0 CHECK (batches_reverted >= 0),
	rows_reverted bigint NOT NULL DEFAULT 0 CHECK (rows_reverted >= 0),
	total_batches bigint CHECK (total_batches IS NULL OR total_batches >= 0),
	last_error_code text CHECK (last_error_code IN ('sql_error', 'connection_error', 'interrupted',
		'revalidation_failed', 'commit_uncertain', 'unsafe_revert')),
	last_error_message text NOT NULL DEFAULT '' CHECK (char_length(last_error_message) <= 1000),
	last_sqlstate text CHECK (last_sqlstate ~ '^[0-9A-Z]{5}$'),
	last_error_retryable boolean NOT NULL DEFAULT false,
	queued_at timestamptz NOT NULL,
	started_at timestamptz,
	finished_at timestamptz,
	UNIQUE (database_id, id),
	FOREIGN KEY (database_id, change_request_id) REFERENCES change_requests (database_id, id),
	CHECK ((lease_owner IS NULL) = (lease_until IS NULL)),
	CHECK (status NOT IN ('queued', 'running') OR finished_at IS NULL),
	CHECK (status NOT IN ('succeeded', 'failed', 'partial', 'partially_reverted') OR finished_at IS NOT NULL),
	CHECK (attempt <= max_attempts),
	CHECK (last_error_code IS NOT NULL OR (last_sqlstate IS NULL AND last_error_message = ''))
);

CREATE INDEX jobs_claim_idx ON jobs (run_after, queued_at, id) WHERE status = 'queued';
CREATE INDEX jobs_lease_idx ON jobs (lease_until) WHERE status = 'running';
CREATE INDEX jobs_database_idx ON jobs (database_id, queued_at DESC, id DESC);
CREATE INDEX jobs_execution_idx ON jobs (execution_id);

ALTER TABLE change_requests ADD CONSTRAINT change_requests_source_job_fkey
	FOREIGN KEY (source_job_id) REFERENCES jobs (id);
ALTER TABLE database_workflows ADD CONSTRAINT database_workflows_blocked_by_job_fkey
	FOREIGN KEY (blocked_by_job_id) REFERENCES jobs (id);

-- Semua attempt dicatat, termasuk yang gagal (spesifikasi revisi 4).
CREATE TABLE job_attempts (
	job_id uuid NOT NULL REFERENCES jobs (id),
	attempt integer NOT NULL CHECK (attempt >= 1),
	database_id uuid NOT NULL,
	change_request_id uuid NOT NULL,
	migration_id uuid NOT NULL,
	migration_revision bigint NOT NULL CHECK (migration_revision > 0),
	execution_id uuid NOT NULL,
	kind text NOT NULL CHECK (kind IN ('apply', 'rollback', 'forward', 'resume', 'revert_batches')),
	direction text CHECK (direction IN ('up', 'down')),
	actor_id uuid NOT NULL REFERENCES users (id),
	actor text NOT NULL,
	worker text NOT NULL CHECK (char_length(worker) BETWEEN 1 AND 200),
	lease_token bigint NOT NULL CHECK (lease_token > 0),
	statements jsonb CHECK (statements IS NULL OR jsonb_typeof(statements) = 'array'),
	status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'partial', 'uncertain',
		'partially_reverted')),
	error_code text CHECK (error_code IN ('sql_error', 'connection_error', 'interrupted',
		'revalidation_failed', 'commit_uncertain', 'unsafe_revert')),
	error_message text NOT NULL DEFAULT '' CHECK (char_length(error_message) <= 1000),
	sqlstate text CHECK (sqlstate ~ '^[0-9A-Z]{5}$'),
	retryable boolean NOT NULL DEFAULT false,
	started_at timestamptz NOT NULL,
	finished_at timestamptz,
	PRIMARY KEY (job_id, attempt),
	FOREIGN KEY (database_id, job_id) REFERENCES jobs (database_id, id),
	FOREIGN KEY (database_id, migration_id) REFERENCES migrations (database_id, id),
	CHECK ((status = 'running') = (finished_at IS NULL)),
	CHECK (status <> 'failed' OR error_code IS NOT NULL)
);

CREATE INDEX job_attempts_database_idx ON job_attempts (database_id, started_at DESC, job_id, attempt);

-- Attempt yang sudah selesai tidak boleh diubah atau dihapus.
CREATE FUNCTION job_attempts_protect() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF TG_OP = 'DELETE' OR OLD.finished_at IS NOT NULL THEN
		RAISE EXCEPTION 'finished attempt is immutable' USING ERRCODE = 'check_violation';
	END IF;
	IF NEW.job_id IS DISTINCT FROM OLD.job_id OR NEW.attempt IS DISTINCT FROM OLD.attempt OR
		NEW.database_id IS DISTINCT FROM OLD.database_id OR
		NEW.change_request_id IS DISTINCT FROM OLD.change_request_id OR
		NEW.migration_id IS DISTINCT FROM OLD.migration_id OR
		NEW.migration_revision IS DISTINCT FROM OLD.migration_revision OR
		NEW.execution_id IS DISTINCT FROM OLD.execution_id OR NEW.kind IS DISTINCT FROM OLD.kind OR
		NEW.actor_id IS DISTINCT FROM OLD.actor_id OR NEW.actor IS DISTINCT FROM OLD.actor OR
		NEW.worker IS DISTINCT FROM OLD.worker OR NEW.lease_token IS DISTINCT FROM OLD.lease_token OR
		NEW.started_at IS DISTINCT FROM OLD.started_at OR
		(OLD.direction IS NOT NULL AND NEW.direction IS DISTINCT FROM OLD.direction) OR
		(OLD.statements IS NOT NULL AND NEW.statements IS DISTINCT FROM OLD.statements)
	THEN
		RAISE EXCEPTION 'attempt identity is immutable' USING ERRCODE = 'check_violation';
	END IF;
	RETURN NEW;
END;
$$;

CREATE TRIGGER job_attempts_protect
	BEFORE UPDATE OR DELETE ON job_attempts
	FOR EACH ROW EXECUTE FUNCTION job_attempts_protect();

-- Cermin checkpoint batch di target untuk progres SPA.
CREATE TABLE job_batches (
	execution_id uuid NOT NULL,
	batch_no bigint NOT NULL CHECK (batch_no >= 1),
	job_id uuid NOT NULL REFERENCES jobs (id),
	cursor_from text CHECK (cursor_from IS NULL OR cursor_from ~ '^-?[0-9]{1,19}$'),
	cursor_to text NOT NULL CHECK (cursor_to ~ '^-?[0-9]{1,19}$'),
	scope_max text NOT NULL CHECK (scope_max ~ '^-?[0-9]{1,19}$'),
	rows bigint NOT NULL CHECK (rows >= 0),
	committed_at timestamptz NOT NULL,
	reverted_at timestamptz,
	reverted_by_job_id uuid REFERENCES jobs (id),
	PRIMARY KEY (execution_id, batch_no),
	CHECK ((reverted_at IS NULL) = (reverted_by_job_id IS NULL))
);

CREATE INDEX job_batches_job_idx ON job_batches (job_id, batch_no);

CREATE FUNCTION job_batches_protect() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF TG_OP = 'DELETE' OR OLD.reverted_at IS NOT NULL OR
		NEW.execution_id IS DISTINCT FROM OLD.execution_id OR NEW.batch_no IS DISTINCT FROM OLD.batch_no OR
		NEW.job_id IS DISTINCT FROM OLD.job_id OR NEW.cursor_from IS DISTINCT FROM OLD.cursor_from OR
		NEW.cursor_to IS DISTINCT FROM OLD.cursor_to OR NEW.scope_max IS DISTINCT FROM OLD.scope_max OR
		NEW.rows IS DISTINCT FROM OLD.rows OR NEW.committed_at IS DISTINCT FROM OLD.committed_at
	THEN
		RAISE EXCEPTION 'committed batch is immutable' USING ERRCODE = 'check_violation';
	END IF;
	RETURN NEW;
END;
$$;

CREATE TRIGGER job_batches_protect
	BEFORE UPDATE OR DELETE ON job_batches
	FOR EACH ROW EXECUTE FUNCTION job_batches_protect();

-- Migration history: hanya operasi yang terbukti ter-commit, append-only.
CREATE TABLE executions (
	id uuid PRIMARY KEY,
	database_id uuid NOT NULL,
	migration_id uuid NOT NULL,
	change_request_id uuid REFERENCES change_requests (id),
	job_id uuid REFERENCES jobs (id),
	backfilled boolean NOT NULL DEFAULT false,
	migration_revision bigint NOT NULL CHECK (migration_revision > 0),
	checksum bytea CHECK (checksum IS NULL OR octet_length(checksum) = 32),
	operation text NOT NULL CHECK (operation IN ('apply', 'rollback', 'forward', 'resume', 'revert_batches')),
	direction text NOT NULL CHECK (direction IN ('up', 'down')),
	reverts_execution_id uuid UNIQUE REFERENCES executions (id),
	forward_of_execution_id uuid REFERENCES executions (id),
	title text NOT NULL,
	up_sql text NOT NULL,
	down_sql text,
	irreversible boolean NOT NULL,
	irreversible_reason text NOT NULL,
	mode text NOT NULL CHECK (mode IN ('normal', 'batch')),
	batch_config jsonb,
	statements jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(statements) = 'array'),
	status text NOT NULL DEFAULT 'succeeded' CHECK (status = 'succeeded'),
	actor_id uuid REFERENCES users (id),
	actor text NOT NULL,
	started_at timestamptz NOT NULL,
	committed_at timestamptz NOT NULL,
	position_version bigint NOT NULL CHECK (position_version > 0),
	FOREIGN KEY (database_id, migration_id) REFERENCES migrations (database_id, id),
	CHECK (backfilled OR (change_request_id IS NOT NULL AND job_id IS NOT NULL AND checksum IS NOT NULL
		AND actor_id IS NOT NULL)),
	CHECK (operation NOT IN ('apply', 'forward') OR direction = 'up'),
	CHECK (operation <> 'rollback' OR (direction = 'down' AND reverts_execution_id IS NOT NULL)),
	CHECK (reverts_execution_id IS NULL OR direction = 'down'),
	CHECK ((operation = 'forward') = (forward_of_execution_id IS NOT NULL)),
	CHECK (committed_at >= started_at)
);

CREATE INDEX executions_history_idx ON executions (database_id, committed_at DESC, id DESC);
CREATE UNIQUE INDEX executions_position_idx ON executions (database_id, position_version);
CREATE INDEX executions_job_idx ON executions (job_id);

ALTER TABLE change_requests ADD CONSTRAINT change_requests_target_execution_fkey
	FOREIGN KEY (target_execution_id) REFERENCES executions (id);

CREATE FUNCTION executions_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	RAISE EXCEPTION 'migration history is append-only' USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER executions_append_only
	BEFORE UPDATE OR DELETE ON executions
	FOR EACH ROW EXECUTE FUNCTION executions_append_only();
CREATE TRIGGER executions_no_truncate
	BEFORE TRUNCATE ON executions
	FOR EACH STATEMENT EXECUTE FUNCTION executions_append_only();
CREATE TRIGGER job_attempts_no_truncate
	BEFORE TRUNCATE ON job_attempts
	FOR EACH STATEMENT EXECUTE FUNCTION executions_append_only();

-- Backfill guard dan riwayat M1 yang applied, berurutan menurut versi.
INSERT INTO database_workflows (database_id) SELECT id FROM managed_databases;

INSERT INTO executions (id, database_id, migration_id, backfilled, migration_revision, operation, direction,
	title, up_sql, irreversible, irreversible_reason, mode, statements, actor_id, actor,
	started_at, committed_at, position_version)
SELECT m.execution_id, m.database_id, m.id, true, m.revision, 'apply', 'up',
	m.title, m.sql, m.irreversible, m.irreversible_reason, m.mode,
	jsonb_build_array(jsonb_build_object('query', m.sql, 'params', '[]'::jsonb, 'batch', NULL)),
	m.actor_id, COALESCE(m.actor, ''),
	COALESCE(m.executed_at, m.updated_at), COALESCE(m.executed_at, m.updated_at),
	row_number() OVER (PARTITION BY m.database_id ORDER BY m.version)
FROM migrations m
WHERE m.status = 'applied';

UPDATE database_workflows w
SET position_version = h.version
FROM (SELECT database_id, max(position_version) AS version FROM executions GROUP BY database_id) h
WHERE w.database_id = h.database_id;
