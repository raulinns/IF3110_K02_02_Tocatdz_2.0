# K1: API Milestone 2

Owner: **13524056**. Frontend reviewer: **13524032**. Store/worker reviewer:
**13524044**. Version: `0.2.0-contract.1`, **review candidate**, not a claim that
the routes are implemented or that team approval has happened.

Start here:

1. [`openapi.yaml`](openapi.yaml): authoritative HTTP shapes, operation IDs,
   permissions (`x-access`), examples, success and error responses.
2. [`../docs/contracts/m2.md`](../docs/contracts/m2.md): K2 semantics, transaction
   boundaries, provisional decisions, state transitions and handoff checklist.
3. [`../src/internal/migration/types_m2.go`](../src/internal/migration/types_m2.go)
   and [`ports_m2.go`](../src/internal/migration/ports_m2.go): compilable Go domain
   contract; deliberately separate from M1 types and HTTP DTOs.
4. [`../docs/observability/log-schema.md`](../docs/observability/log-schema.md): K5.
5. [`examples/`](examples/): deterministic fixtures for MSW, UI and backend tests.

## Frontend quick start (13524032)

Use stable `operationId` names for MSW/client functions. Generate TypeScript types
from this OpenAPI document using the frontend's pinned `openapi-typescript`
dependency. For example, after installing that dependency in `web/`:

```sh
cd web
npx openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts
```

The frontend owns dependency installation and generated files. No frontend source
or build dependency is added by the contract PR. Examples are fixtures, not live
credentials or proof of working backend handlers.

All application routes are relative to `/api/v1`. `/healthz` and `/metrics` override
the server prefix and are root-level internal operations endpoints. Production
and dev use same-origin fetch. Dev Vite proxies `/api` to Go and Go uses
`PUBLIC_ORIGIN=http://localhost:5173`; do not solve this with permissive CORS.

```ts
// Schema imports omitted; keep csrf in memory, never localStorage.
const meResponse = await fetch('/api/v1/auth/me', { credentials: 'same-origin' });
if (!meResponse.ok) throw new Error('Handle the documented error envelope');
const me = await meResponse.json();
const response = await fetch(`/api/v1/databases/${databaseID}/change-requests/${crID}/execute`, {
  method: 'POST',
  credentials: 'same-origin',
  headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token },
  body: JSON.stringify({ revision: approvedCR.revision }),
});
// 202 means accepted, NOT SQL committed. Follow Location or job_id and poll.
```

Login is the token-exempt bootstrap: POST JSON credentials with a valid browser
Origin. It returns the same schema as `/auth/me` and sets the HttpOnly session
cookie. Logout and all other authenticated unsafe methods require the CSRF
header, including requests with no body. Refresh `/auth/me` after `csrf_failed`;
do not automatically replay a write with unknown outcome. Session cookies may
rotate on any authenticated response; the browser handles `Set-Cookie`.

Lists return `items`, `page`, `limit`, `has_next`. Migration/execution lists return
summaries; fetch detail for SQL and the paginated execution /statements endpoint
for expanded batch SQL. Rows use `items` too (the adapter
renames M1's `rows`). Catalogs are bounded snapshots, not paginated lists. Empty
arrays are `[]`; nullable fields are explicitly `null`; unsafe JSON integers are
not accepted (maximum `2^53-1`). Identifier lengths/SQL sizes additionally have
UTF-8 byte limits enforced by Go, as described in the schema.

Use the returned CR revision after each decision; do not assume that two leaders
can submit decisions using the same stale revision. `group_id` disambiguates a
leader who leads multiple required groups. Display each group's decision.
`leader_of` and position `can_*` fields aid UI rendering; the backend remains the
authority. A viewer leader can approve for their group but cannot execute.

Poll jobs while `queued`/`running` (e.g. every two seconds). Stop automatic polling
on other statuses and show a recovery/reconciliation action or manual refresh.
`uncertain` may later be reconciled; it is not evidence of failure. Unknown totals
are `null`, never an invented percent. Read failures from `/jobs/{job_id}/attempts`;
execution history intentionally contains only committed operations.

## Backend/store quick start (13524044)

Implement the K2 interfaces in your own packages. Consumers write local fakes;
there is no shared fake singleton. `WorkflowStore.WithWorkflow` is the transaction
boundary for definitions, snapshots, approvals and enqueue. Never call queue
insert and CR transition as separate transactions. `JobQueue` is intentionally a
read port; enqueue is only available on `WorkflowTx`.

Worker claim/lease bookkeeping belongs to `WorkerStore`. Use receipt identities
and a lease fencing token. Target SQL and platform DB cannot commit atomically;
reconcile receipts/checkpoints before scheduling retry. Never implement a retry
by rerunning SQL solely because the platform history row is missing.

K2 specifies capability boundaries, not SQL implementation. The worker owner
defines batch-specific locked-target operations around the same connection as
M1's `LockedTarget`. Do not implement batch transactions by acquiring another pool
connection. K3/K4 schema and SQL are owned by 13524044 and require review against
the explicit handoff checklist in the domain contract.

## Validation and changes

From `src/`:

```sh
go test ./internal/migration -run '^TestM2Contract' -count=1
go test ./...
go vet ./...
```

Optional standards validation from the repo root (does not add runtime Go deps):

```sh
npx @redocly/cli@2.14.7 lint api/openapi.yaml --config api/redocly.yaml
```

On Windows PowerShell with script execution disabled, use `npx.cmd`. CI should
use the same pinned tool version when the integrator adds validation to CI. Go tests
guard references, route/security completeness, enum parity and canonical fixtures;
they complement a standards validator rather than replace it.

Review K1/K2/K5 together before freeze. Subsequent changes use a PR labeled
`contract` reviewed by all three members; bump the contract version and update
fixtures/tests/docs together. No approval/freeze is implied by creating these files.
