-- Attempt history is independent from the committed execution stack.
ALTER TABLE job_attempts ADD COLUMN history_id uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE job_attempts ADD CONSTRAINT job_attempts_history_id_key UNIQUE (history_id);
CREATE INDEX attempt_history_page_idx ON job_attempts(database_id,started_at DESC,history_id DESC);
ALTER TABLE job_attempts ADD COLUMN definition_snapshot jsonb;
ALTER TABLE job_attempts ADD CONSTRAINT attempt_snapshot_object CHECK
    (definition_snapshot IS NULL OR jsonb_typeof(definition_snapshot) = 'object');

ALTER TABLE change_requests ADD COLUMN retry_of_job_id uuid;
ALTER TABLE change_requests ADD CONSTRAINT change_requests_retry_source_fk
    FOREIGN KEY (database_id, retry_of_job_id) REFERENCES jobs(database_id,id);
ALTER TABLE change_requests ADD CONSTRAINT retry_is_normal_operation CHECK
    (retry_of_job_id IS NULL OR (source_job_id IS NULL AND kind IN ('apply','rollback','forward')));
CREATE UNIQUE INDEX one_manual_retry_per_source ON change_requests(retry_of_job_id)
WHERE retry_of_job_id IS NOT NULL AND status <> 'rejected';

CREATE FUNCTION attempt_history_identity_protect() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.history_id IS DISTINCT FROM OLD.history_id OR
       NEW.definition_snapshot IS DISTINCT FROM OLD.definition_snapshot THEN
        RAISE EXCEPTION 'attempt history snapshot is immutable' USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER attempt_history_identity_protect BEFORE UPDATE ON job_attempts
FOR EACH ROW EXECUTE FUNCTION attempt_history_identity_protect();

CREATE FUNCTION retry_identity_protect() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.retry_of_job_id IS DISTINCT FROM OLD.retry_of_job_id THEN
        RAISE EXCEPTION 'retry source is immutable' USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER retry_identity_protect BEFORE UPDATE ON change_requests
FOR EACH ROW EXECUTE FUNCTION retry_identity_protect();

-- A started record proves dispatch intent, not delivery/commit. A result is
-- appended separately; a crash can legitimately leave the result unavailable.
CREATE TABLE attempt_statements (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    history_id uuid NOT NULL REFERENCES job_attempts(history_id),
    statement jsonb NOT NULL CHECK (jsonb_typeof(statement)='object'),
    started_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX attempt_statements_page_idx ON attempt_statements(history_id,id);
CREATE TABLE attempt_statement_results (
    statement_id bigint PRIMARY KEY REFERENCES attempt_statements(id),
    status text NOT NULL CHECK (status IN ('succeeded','failed')),
    sqlstate text CHECK (sqlstate ~ '^[0-9A-Z]{5}$'),
    finished_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION attempt_statement_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'attempt statement records are append only' USING ERRCODE='check_violation';
END;
$$;
CREATE TRIGGER attempt_statement_append_only BEFORE UPDATE OR DELETE ON attempt_statements
FOR EACH ROW EXECUTE FUNCTION attempt_statement_append_only();
CREATE TRIGGER attempt_result_append_only BEFORE UPDATE OR DELETE ON attempt_statement_results
FOR EACH ROW EXECUTE FUNCTION attempt_statement_append_only();

-- Reconciliation resolves a finished uncertain attempt without rewriting it.
CREATE TABLE attempt_resolutions (
    history_id uuid PRIMARY KEY REFERENCES job_attempts(history_id),
    status text NOT NULL CHECK(status IN ('succeeded','failed','partial','partially_reverted')),
    recorded_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER attempt_resolution_append_only BEFORE UPDATE OR DELETE ON attempt_resolutions
FOR EACH ROW EXECUTE FUNCTION attempt_statement_append_only();
CREATE TRIGGER attempt_statement_no_truncate BEFORE TRUNCATE ON attempt_statements
FOR EACH STATEMENT EXECUTE FUNCTION attempt_statement_append_only();
CREATE TRIGGER attempt_result_no_truncate BEFORE TRUNCATE ON attempt_statement_results
FOR EACH STATEMENT EXECUTE FUNCTION attempt_statement_append_only();
CREATE TRIGGER attempt_resolution_no_truncate BEFORE TRUNCATE ON attempt_resolutions
FOR EACH STATEMENT EXECUTE FUNCTION attempt_statement_append_only();
