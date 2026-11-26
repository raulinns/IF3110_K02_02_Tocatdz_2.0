CREATE TABLE batch_statements (
    job_id uuid NOT NULL REFERENCES jobs(id),
    batch_no bigint NOT NULL CHECK (batch_no > 0),
    statement jsonb NOT NULL CHECK (jsonb_typeof(statement) = 'object'),
    PRIMARY KEY (job_id, batch_no)
);

CREATE TRIGGER batch_statements_append_only
    BEFORE UPDATE OR DELETE ON batch_statements
    FOR EACH ROW EXECUTE FUNCTION executions_append_only();

CREATE TRIGGER batch_statements_no_truncate
    BEFORE TRUNCATE ON batch_statements
    FOR EACH STATEMENT EXECUTE FUNCTION executions_append_only();
