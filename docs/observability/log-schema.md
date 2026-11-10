# K5: structured logging and tracing contract

Version `0.2.0-contract.1`, review candidate. Owner **13524056**; consumers all
lanes. Required instrumentation comes from PDF p17. Collector, central storage,
Prometheus and Grafana are bonus owned by 13524044. This document defines the
future helper behavior; it does not claim a logger/exporter already exists.

## Encoding, correlation and safety

Emit one UTF-8 JSON object per line to stdout, using Go slog. Required keys exist
on every application event; use null when unavailable, not a fictitious ID. OTel's
console exporter may use its own JSON span format, but never mix unstructured
debug prints with application log output. No application log table in platform DB.

| Field | Type / meaning |
|---|---|
| time | RFC 3339 UTC (fractional seconds allowed) |
| level | DEBUG, INFO, WARN, ERROR |
| msg | Safe short message, no secrets interpolated |
| event | Stable name from the event catalog below |
| actor | Active human user's UUID, worker:<worker-id>, anonymous, or system |
| database_id | UUID or null (login/global operations) |
| status | start, succeeded, failed, queued, running, partial, uncertain, partially_reverted, approved, rejected, committed, rolled_back, released, scheduled |
| request_id | Server-generated opaque correlation ID or null for unrelated background work |
| trace_id | 32 lowercase hex characters or null when no span context exists |
| span_id | 16 lowercase hex characters or null when no span context exists |

HTTP events additionally include `http_method`, route **template** (`http_route`),
`http_status` and `duration_ms`. Generate a fresh request ID; replace untrusted
incoming X-Request-ID. Do not log raw URLs/querystrings, cookies, request/response
bodies, Authorization, CSRF header, password, DSN or session identifiers. Login
actor is anonymous on failure; do not log submitted username/password.

| SQL / workflow field | Type / meaning |
|---|---|
| query | Actual driver SQL with literal data redacted, retaining structure/placeholders |
| params | Array; values always [REDACTED], never fmt of raw parameters |
| duration_ms | Nonnegative number; null at start before timing is available |
| rows | Nonnegative integer or null when driver does not supply it |
| error | Safe allowlisted message or null, never raw driver Error() |
| sqlstate | Five-character SQLSTATE or null |
| migration_id, change_request_id, job_id, execution_id | Relevant UUID or null |
| migration_revision | Reviewed positive revision or null |
| direction | up, down, or null for read-only SQL |
| attempt | One-based attempt, or null if unrelated |
| batch | One-based batch, or null for normal/read-only SQL |

Log catalog queries, platform store SQL and read-only explorer/plan/schema-diff
queries too, not only worker SQL. Parameter values and literals may hold sensitive
data. Use a SQL-aware redactor (including dollar quotes/comments); regex-only
redaction is insufficient. On parse/redaction failure, emit `[REDACTED SQL]`, not
the original string. Cap query output at 16 KiB and mark `query_truncated=true`.
Query fingerprint/hash is optional and cannot replace the query field entirely.
Protected history retains exact reviewed/executed SQL; log output redacts data.
Never strip credentials by logging the full SQL first and sanitizing afterward.

Do not log domain structs through reflection: `security.Secret` intentionally
prevents accidental serialization. The helper must explicitly reveal SQL only
into a redactor; HTTP DTO mapping and logging redaction are different operations.
Sanitize driver errors before building slog attributes or span events/status.

## Event catalog

| Events | Producer / boundary |
|---|---|
| http.request | API completion, including failed auth/CSRF |
| auth.login, auth.logout | Auth result; never identity secrets |
| admin.leader_changed, admin.grant_changed, admin.membership_changed | Admin committed mutations |
| migration.created, migration.updated, migration.submitted | Definition/workflow commit |
| migration.approved, migration.rejected | Each persisted group decision (group_id included) |
| job.enqueued | After atomic enqueue transaction commits, not before |
| job.claimed | Worker lease acquired; attempt and worker_id |
| lock.acquire.start, lock.acquired, lock.released | Lock wait start, success and release; duration_ms for wait |
| job.revalidated | Revalidation success/failure before target mutation |
| sql.query.start, sql.query | Read-only/platform SQL start/result |
| sql.exec.start, sql.exec.done | Target mutation start/result |
| tx.commit.start, tx.commit, tx.rollback.start, tx.rollback | Transaction begin-commit/rollback and outcome; distinguish uncertain commit |
| batch.start, batch.commit, batch.rollback | Batch lifecycle; committed counters only after receipt/checkpoint proof |
| batch.revert.start, batch.revert.commit, batch.revert.rollback | Compensating batch lifecycle |
| job.retry_scheduled | Prior failed attempt and persisted next run_after; never implies SQL committed |
| job.succeeded, job.failed, job.partial, job.uncertain, job.partially_reverted | Worker outcome, separately from individual statement outcome |
| job.reconciled | Receipt/checkpoint reconciliation result |
| history.appended | Platform history transaction committed; execution ID required |

`sql.exec.done` succeeding does not imply a committed transaction. Emit commit/
rollback separately. A crash may omit an end event: do not fabricate success on
startup; emit job.reconciled after proof. Include attempt SQL revision, actor and
execution identity for rollback, forward, retry and failures too.

## Span tree and queue carrier

Use OTel Go SDK with W3C TraceContext propagation. HTTP middleware uses the route
template, not raw URL. Internal same-origin deployment does not promise acceptance
of arbitrary browser trace headers: validate input and apply trust policy. Never
put sensitive data into baggage or span names. No baggage propagated via jobs.

```text
HTTP POST /api/v1/databases/{database_id}/change-requests/{change_request_id}/execute
  job.enqueue                          (producer)
    job.process                        (consumer; starts later, same trace)
      lock.acquire
      revalidate
      sql.exec                         (normal)
        db.query
      batch.process                    (batch; batch number is an attribute)
        db.query
      history.append
```

HTTP producer can finish before consumer starts; parent-child trace context
still applies. Each retry/resume has its own processing span and links to the
source context if it represents a new CR/job. Never reuse a finished span ID.

Future helper signatures:

```go
// package tracing; these are the handoff contract, not yet implementations.
func Inject(ctx context.Context) string
func Extract(carrier string) context.Context
```

`Inject` emits compact JSON with allowlisted `traceparent` and optional
`tracestate`; empty string means no valid span context. Example:

```json
{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
```

Store the carrier internally as `JobSpec.TraceContext`, never HTTP job JSON.
Maximum carrier size 4096 UTF-8 bytes. Extract validates W3C format/size; malformed
or absent input yields a background context with no parent, not an error that
prevents processing. Worker adds its own cancellation/deadline to extracted
context. Do not copy the carrier wholesale into logs. K5 helpers add trace/span
IDs from the active context, not from untrusted string attributes.

For no-op tracing before instrumentation is ready, consumers use empty carriers
and null IDs; they must not invent a trace. Once enabled, configure console export
by default and OTLP when OTEL_EXPORTER_OTLP_ENDPOINT is set; shutdown flushes spans
within a bounded deadline. Dependency/config wiring is owned by 13524044.

Spans include duration, error status and safe events. Keep SQL, params and errors
under the same redaction policy as logs. Prometheus labels are **not** these IDs:
only bounded status/kind/mode/pool labels belong in metrics.

## Illustrative fixture (not evidence of runtime instrumentation)

The following line demonstrates a successful statement followed by a transaction
whose commit is not yet proven. It must not create a successful history row.

```json
{"time":"2026-11-10T10:00:00Z","level":"INFO","msg":"Statement completed","event":"sql.exec.done","actor":"30000000-0000-4000-8000-000000000001","database_id":"10000000-0000-4000-8000-000000000001","status":"succeeded","request_id":"example-request-id","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","query":"UPDATE public.orders SET processed = $1 WHERE id = $2","params":["[REDACTED]","[REDACTED]"],"duration_ms":12.5,"rows":1,"error":null,"sqlstate":null,"migration_id":"20000000-0000-4000-8000-000000000001","change_request_id":"50000000-0000-4000-8000-000000000001","job_id":"60000000-0000-4000-8000-000000000001","execution_id":"70000000-0000-4000-8000-000000000001","migration_revision":1,"direction":"up","attempt":1,"batch":null}
```

Actual runtime examples for login, execute, batch, retry, failure and matching
console spans belong to TRC-03's later `log-trace-examples.md`. Do not present these
contract illustrations as scan/test/observability evidence.
