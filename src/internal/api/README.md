# JSON API foundation (API-01 / SEC-01)

This package implements K1's transport boundary. Production auth/admin/explorer
and workflow endpoints are later tasks; no placeholder success routes are mounted.
With no routes configured, `/api/v1` and its subtree return JSON 404.

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

Transport integration tests use real auth/session services, CSRF, token encryption,
cookie rotation, limiter and backend authorizer with in-memory persistence. They
are not substitutes for later real-DB API integration tests. The tests' illustrative
routes are not registered by production API.New. No new Go dependencies are needed.
