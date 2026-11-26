# JSON API foundation, authentication, admin and explorer

This package implements K1's transport boundary, authentication, admin and
read-only explorer. Without Auth or custom Routes, the API subtree returns JSON 404.

## Authentication module (API-02)

Enable all three production endpoints with an explicit leadership dependency:

```go
frontend, err := api.New(api.Config{
    Security: securityConfig, // M1 AuthService, checked logout, CSRF, cookies, limiter
    Logger: logger,
    Auth: &api.AuthConfig{Leaders: leaderDirectory},
})
if err != nil { /* fail application startup */ }
handler := web.NewRouter(logger, frontend)
```

The integrator (13524044) owns this application wiring. `leaderDirectory` implements
the consumer-owned `GroupLeadership.LeaderGroups(ctx, userID)` interface. K2's
LeaderDirectory already includes that method, so no migration-to-API adapter is
needed. It must query the current active group leadership/membership; do not
install an always-empty implementation in production to hide missing M2 storage.
Missing or typed-nil dependencies fail at construction. `Auth:nil` intentionally
leaves the module disabled for transport-only configurations and their tests.
Built-in auth paths cannot be replaced by custom Routes, including public HEAD.

| Endpoint | Behavior |
|---|---|
| POST /api/v1/auth/login | Origin-protected, JSON <=16 KiB, peer-IP + account throttling, cookie session; no pre-login CSRF token |
| GET /api/v1/auth/me | Authenticate/rotate session, read current leadership, return K1 AuthMe |
| POST /api/v1/auth/logout | No query/body, valid Origin + header CSRF, checked session revocation, expired cookie and 204 (no redirect) |

Login and /me use the same explicit SessionResponse DTO: `user` (id, username,
active, platform_admin), top-level platform_admin, csrf_token and leader_of. Active
is true because the service verified the account. Username normalization belongs
to M1; password bytes are never trimmed. Required/size validation is 422, wrong
credentials/unknown user/disabled account share a generic 401. Leader IDs are
validated, copied, sorted and deduplicated; no leadership is `[]`, never null.
Leadership is a UI hint; admin/DB operations still require backend authorization.

A directory failure/invalid result produces a safe 503, never fabricated empty
leadership. Authentication may already have committed/replaced a session before
that read. Login therefore sends the committed session cookie even on this later
503; /me likewise preserves rotated cookies on profile failures. Clients should
show the temporary error and retry /me, not discard the session or infer success
from the cookie alone. No response/URL/log exposes a session identifier/password.

Auth events use the injected logger and K5 field names. `auth.login` succeeded
means the authentication/session operation committed, even if a later profile
read makes the HTTP response 503. Logout uses the existing checked-revocation
handler once, avoiding a redundant Authenticate/rotation immediately before
revocation. Failed login uses actor=anonymous; logout records only the verified
actor. Log text/error codes exclude raw dependency errors, cookie/CSRF values
and request bodies. Trace/span fields remain null until TRC instrumentation.

## Admin module (API-03)

Append the routes to the same API configuration used for authentication.
The integrator constructs the two services from the platform repositories:

~~~go
m2Admin, err := admin.NewM2Service(adminService, m2AdminRepository)
if err != nil { /* fail application startup */ }
routes, err := api.AdminRoutes(api.AdminConfig{
    Service: adminService,
    M2:      m2Admin,
    Logger:  logger,
})
if err != nil { /* fail application startup */ }
cfg.Routes = append(cfg.Routes, routes...)
~~~

All 17 operations use session authentication and the backend platform-admin
authorizer. Mutations use the JSON transport's Origin/CSRF guard. Lists use K1
ordering and pagination. Create responses include the committed public record
and a Location header. Passwords and DSNs are input-only.

Omit M2 to expose the seven routes backed by the existing admin service:
list/create users, create group, list/create connections, and list/revoke grants.
Register the full module after the platform store implements
admin.M2Repository. That port defines the transaction requirements for returning
mutation results, resetting passwords, changing membership or leadership, and
rejecting tombstones that would affect unresolved workflow. Reuse the M2-aware
platform store for M1-backed grant revocation so it follows workflow locking too.
The admin tests use real session, CSRF and authorizer components with in-memory
persistence. Use real PostgreSQL integration tests to verify those store guarantees.

## Explorer module (API-04)

Use the existing explorer and schema-diff services and the target router:

```go
routes, err := api.ExplorerRoutes(api.ExplorerConfig{
    Viewer: explorerService,
    Diff: schemaDiffService,
    Engines: targetRouter,
})
if err != nil { /* fail application startup */ }
cfg.Routes = append(cfg.Routes, routes...)
```

The integrator passes `cfg` to `api.New`. The routes all require a session.
Database listing paginates the authorized catalog in name and ID order. Engine
capabilities describe database compatibility. The frontend can use them together
with the user's access level to decide which actions to show. Table and row reads
use the explorer service's grant checks. Schema comparison requires read access
to both PostgreSQL targets and returns SQL for review.

## Query plan (API-04)

Construct the planner with the M2 definition store and the existing PostgreSQL
validator and target router, then append its route to the same API configuration:

~~~go
planner, err := migration.NewDefinitionPlanService(migration.DefinitionPlanConfig{
    Definitions: m2Store,
    Authorizer:  authorizer,
    Coordinator: targetRouter,
    Validator:   postgresValidator,
})
if err != nil { /* fail application startup */ }
routes, err := api.QueryPlanRoutes(planner)
if err != nil { /* fail application startup */ }
cfg.Routes = append(cfg.Routes, routes...)
~~~

POST /api/v1/databases/{database_id}/migrations/{migration_id}/plan requires
a session, CSRF and view_edit. It uses the saved revision and requested up/down
direction for normal-mode PostgreSQL definitions, including frozen definitions.
The response identifies the full definition checksum and contains the allowlisted
plan tree and optional timings. ANALYZE is limited to read-only SELECT. A revision
change during planning yields 409. Target analysis creates no queue jobs or history
entries. Server wiring and validation against stored definitions belong to M2
store integration by 13524044.

## Draft and approval workflow (DOM-03, API-05)

Build the service with the existing PostgreSQL M2 store and append the routes
in bootstrap alongside admin, explorer and plan:

~~~go
service, err := migration.NewWorkflowService(migration.WorkflowConfig{
    Store: workflow, Authorizer: authorizer, Engines: targetRouter, IDs: tokens, Clock: clock,
})
if err != nil { /* fail application startup */ }
workflowRoutes, err := api.WorkflowRoutes(service)
if err != nil { /* fail application startup */ }
routes = append(routes, workflowRoutes...)
~~~

The module exposes migration drafts, change requests, approval decisions,
execute and job status. Submission freezes the validated draft and snapshots
the leader of each granted group in one transaction. Execute persists one
job per approved request and returns its Location and current status.
The worker owns target execution. Rollback submits the current reversible
execution and forward submits the first execution in the redo chain. Both
use the original snapshot and follow the same approval and queue flow as apply.

History and job reads use the same authorized workflow service. The store
applies hide_rollback by execution ID before paging. Execution detail exposes
the committed SQL template for review. The statements endpoint uses the
store's paginated statement records and redacts literals, comments and parameters.
Expanded batch statements require the store to expose the target checkpoint
statements through that same paginated port.

## Construct and integrate

The integrator supplies real M1 security dependencies and a slog logger. Routes
are fixed at construction; subsequent requests do not mutate the route registry.
The existing `web.NewRouter(logger, registrars...)` accepts this registrar, so
bootstrap/router wiring can remain owned by 13524044.

```go
frontend, err := api.New(api.Config{
    Security: securityConfig,
    Logger: logger,
    Routes: []api.Route{
        {
            Method: http.MethodPost,
            Path: "/example", // illustrative; not a production K1 endpoint
            Access: api.Authenticated,
            Body: api.JSONBody,
            Handle: func(w http.ResponseWriter, r *http.Request) error {
                var input struct {
                    Title string `json:"title"`
                }
                if err := api.DecodeJSON(w, r, &input); err != nil {
                    return err
                }
                // Call domain service: validate semantics, authorize the action,
                // perform its transaction. Return its safe error on failure.
                return api.WriteJSON(w, http.StatusOK, struct {
                    Title string `json:"title"`
                }{Title: input.Title})
            },
        },
    },
})
if err != nil { /* fail application startup */ }
handler := web.NewRouter(logger, frontend)
```

This example demonstrates transport only, not database write authorization. A
session-authenticated route must still call the domain authorizer for admin/DB
actions. Default Access is Authenticated, default Body is NoBody, and no query
parameters are allowed unless `QueryParameters` explicitly lists them. Public
access is restricted to safe methods. Login access requires POST + JSONBody and
keeps Origin validation and peer-IP rate limiting; it is not a general write bypass.

## JSON input and response rules

- Use explicit DTO structs with snake_case json tags. Do not bind domain structs,
  `security.Principal`, secrets, embedded-field conflicts or custom unmarshalers
  directly to client input. Required fields whose absence matters should use
  pointers and then domain validation.
- A single UTF-8 JSON object is accepted. Unknown or differently-cased keys,
  duplicate keys (even escaped/nested), trailing documents, invalid types and
  excess nesting fail with bad_request. Domain/SQL validation uses 422.
- Body budget is 2 MiB; login additionally retains M1's 16 KiB budget, including
  chunked bodies. Only application/json with optional charset=utf-8 is accepted.
- NoBody operations reject even `{}` before side effects. They do not require a
  Content-Type, but authenticated unsafe methods always need X-CSRF-Token.
- ParseQuery rejects unknown/repeated names and invalid URL escaping. PathID
  checks lowercase nonzero UUID syntax. Authorization and cross-database lookup
  scoping remain the service's responsibility.
- DecodeJSON commits decoded values only on successful parsing. It does not
  distinguish absent required fields unless the DTO/domain explicitly does so.
- WriteJSON encodes before committing headers. Encoding a secret or invalid DTO
  fails without partial output; do not log input/response values on that error.
- Endpoints return errors to the boundary, not `http.Error` or HTML redirects.
  For 204, explicitly call WriteHeader(http.StatusNoContent) and return nil.
  A nil error with no response is treated as a server error rather than empty 200.
- ValidationError.Fields is map[string][]string as K1 specifies. Messages must be
  trusted user-facing text, never raw decoder/SQL errors or submitted values.

## Middleware and M1 coexistence

API.New copies HTTPMiddlewareConfig and selects JSONTransport, WriteError and
the K1 status mapper. M1 constructors default to HTMLTransport, preserving form
login and redirect logout. JSONTransport requires an explicit writer/status mapper
when the security middleware is constructed directly.

JSON CSRF validation reads only the header, leaving JSON body untouched. Tokens
in query/form fields do not substitute for it. Protected responses still refresh
the browser cookie after authentication/session rotation, even if a subsequent
domain action is denied. JSON logout uses checked revocation and 204; missing or
expired sessions produce 401, without pretending the session is still active.

security.WithRequestID/RequestIDFromContext share a trusted server ID with the
existing web middleware. Inbound X-Request-ID is ignored, and error envelope ID
matches the response header. Shared SecurityHeaders keeps the same-origin CSP,
nosniff/referrer policy and other M1 headers consistent; no CORS is added. The API
does not loosen CSP for Vite development; use the agreed same-origin proxy setup.

Unknown paths/methods within the API return JSON 404/405 with Allow when applicable.
Panic recovery never logs the panic value, bodies or credentials. Before response
commit it returns a safe JSON 500; after commit it aborts the connection with
http.ErrAbortHandler. The outer web recovery must propagate that abort instead of
appending text to an incomplete response. Streaming/hijacked endpoints are not
part of this JSON API contract.

## Verification

From `src/`:

```sh
go test ./internal/api ./internal/security ./internal/web -count=1
go test ./... -count=1
go vet ./...
```

Transport and production-auth-route integration tests use real auth/session services, CSRF, token encryption,
cookie rotation, limiter and backend authorizer with in-memory persistence. They
are not substitutes for later real-DB API integration tests. The tests' illustrative
routes are not registered by production API.New; Config.Auth registers the actual
auth endpoints. Tests also cover login replacement, disabled/expired/revoked
sessions, stale CSRF epochs, directory/storage failures and both rate limits.
No new Go dependencies are needed.
