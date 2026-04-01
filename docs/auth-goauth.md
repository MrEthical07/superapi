# Auth & goAuth Integration

How authentication works in SuperAPI with direct goAuth engine integration: auth modes, token flow, route protection, and local development setup.

---

## Architecture

Authentication uses goAuth as the single engine with a thin SuperAPI integration layer:

| Layer | File | Role |
|---|---|---|
| **goAuth engine builder** | `internal/core/auth/goauth_provider.go` | Builds `*goauth.Engine` from config + Redis + SQLC user provider |
| **AuthContext** | `internal/core/auth/context.go` | Authenticated user data injected into request context |
| **AuthRequired policy** | `internal/core/policy/auth.go` | Per-route middleware that enforces authentication |

---

## Enabling authentication

**Required env vars:**

```
AUTH_ENABLED=true
REDIS_ENABLED=true    # goAuth uses Redis for session storage
POSTGRES_ENABLED=true
POSTGRES_URL=postgres://user:pass@localhost:5432/mydb?sslmode=disable
```

**Auth configuration:**

| Env var | Default | Description |
|---|---|---|
| `AUTH_ENABLED` | `false` | Master toggle |
| `AUTH_MODE` | `hybrid` | Default auth mode: `jwt_only`, `hybrid`, `strict` |

Notes:
- In this template, startup config exposed via `internal/core/config/config.go` is currently `AUTH_ENABLED` + `AUTH_MODE`.
- The goAuth engine is built from defaults inside `internal/core/auth/goauth_provider.go`.

---

## Auth modes

The auth mode determines how token validation is performed and is set globally via `AUTH_MODE`, but can be overridden per-route.

### jwt_only

```
AUTH_MODE=jwt_only
```

- Validates JWT signature and claims (issuer, audience, expiry)
- Does NOT check Redis session state
- Fastest mode — no Redis round-trip during auth
- Cannot detect revoked tokens until JWT expires
- Use for: read-heavy endpoints where slight staleness is acceptable

### hybrid (default)

```
AUTH_MODE=hybrid
```

- Validates JWT first
- If Redis is available, also checks session is active
- If Redis is unavailable, falls back to JWT-only
- Balanced: catches revocations when possible, stays available when Redis is down
- Use for: most endpoints

### strict

```
AUTH_MODE=strict
```

- Requires both valid JWT AND active Redis session
- Fails closed: if Redis is unavailable, auth fails with 401
- Most secure: guarantees revoked tokens are rejected immediately
- Use for: sensitive operations (payments, admin actions, tenant data mutations)

### Per-route override

```go
// Module binds the default mode from config
func (m *Module) BindDependencies(d *app.Dependencies) {
    m.authEngine = d.AuthEngine
    m.authMode = d.AuthMode  // Global default from AUTH_MODE
}

// Override for a specific sensitive route
r.Handle(http.MethodPost, "/api/v1/payments", handler,
    policy.AuthRequired(m.authEngine, auth.ModeStrict),  // Override to strict
)
```

---

## Token flow

### Request → Response

```
Client                         SuperAPI                        goAuth Engine
  │                              │                                  │
  │  Authorization: Bearer <jwt> │                                  │
  │─────────────────────────────>│                                  │
  │                              │  engine.Validate(token, mode)    │
  │                              │─────────────────────────────────>│
  │                              │                                  │
  │                              │  AuthContext{UserID, TenantID,   │
  │                              │    Role, Permissions}            │
  │                              │<─────────────────────────────────│
  │                              │                                  │
  │                              │  ctx = auth.WithContext(ctx, ac) │
  │                              │  next.ServeHTTP(w, r.WithCtx)   │
  │                              │                                  │
  │  200 OK + response body      │                                  │
  │<─────────────────────────────│                                  │
```

### Token extraction

The `AuthRequired` policy extracts the token from the `Authorization` header:

```
Authorization: Bearer eyJhbGciOiJIUzI1N...
```

- Only `Bearer` scheme is accepted
- Missing header → 401
- Non-Bearer scheme → 401
- Empty token after `Bearer ` → 401

### AuthContext injection

On successful validation, goAuth middleware returns an auth result and SuperAPI injects `AuthContext` into the request context:

```go
type AuthContext struct {
    UserID      string   // e.g., "usr_abc123"
    TenantID    string   // e.g., "tnt_xyz789" (may be empty)
    Role        string   // e.g., "admin"
    Permissions []string // e.g., ["system.whoami", "project.write"]
}
```

---

## Reading auth context in handlers

```go
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
    principal, ok := auth.FromContext(r.Context())
    if !ok {
        // Should not happen after AuthRequired policy
        response.Error(w, r, apperrors.Unauthorized("not authenticated"))
        return
    }

    // Use principal fields
    userID := principal.UserID
    tenantID := principal.TenantID
    role := principal.Role
    perms := principal.Permissions
}
```

### Reading tenant ID (convenience)

For tenant-scoped operations, you can use the tenant helper:

```go
import "github.com/MrEthical07/superapi/internal/core/tenant"

tenantID, ok := tenant.TenantIDFromContext(r.Context())
if !ok {
    response.Error(w, r, apperrors.Forbidden("tenant scope required"))
    return
}
```

This reads from the same `AuthContext` — it's a shortcut that extracts only the tenant ID.

---

## goAuth engine integration

### What goAuth provides

goAuth is the auth and RBAC engine (`github.com/MrEthical07/goAuth`) and handles:

- JWT creation and validation
- Session management in Redis
- Token refresh
- Session revocation
- Multi-session tracking

### Engine initialization

The goAuth engine is initialized in `internal/core/auth/goauth_provider.go`:

```go
func NewGoAuthEngine(redisClient redis.UniversalClient, mode Mode, userProvider goauth.UserProvider) (*goauth.Engine, func(), error)
```

Runtime wiring details in this template:

- Uses `goauth.DefaultConfig()` and sets validation mode from `AUTH_MODE`.
- Wires Redis client + SQL-backed user provider (`auth.NewSQLCUserProvider(...)`).
- Enables role and permission extraction in auth results.

`policy.AuthRequired(engine, mode)` now calls goAuth middleware guard directly and stores both goAuth auth result and `auth.AuthContext` on request context for downstream handlers.

### User provider

goAuth requires a `UserProvider` during validation. This template uses the DB-backed implementation:

- `internal/core/auth/provider_sqlc.go` (`SQLCUserProvider`)
- wired in `internal/core/app/deps.go` via `auth.NewSQLCUserProvider(db.NewQueries(deps.Postgres))`

This is why `AUTH_ENABLED=true` requires both Redis and Postgres at startup.

---

## Route protection patterns

### Basic authenticated route

```go
r.Handle(http.MethodGet, "/api/v1/profile", handler,
    policy.AuthRequired(m.authEngine, m.authMode),
)
```

### Role-restricted route

```go
r.Handle(http.MethodPost, "/api/v1/admin/users", handler,
    policy.AuthRequired(m.authEngine, auth.ModeStrict),
    policy.RequirePerm("admin.users.write"),
)
```

### Permission-restricted route

```go
r.Handle(http.MethodPost, "/api/v1/projects", handler,
    policy.AuthRequired(m.authEngine, m.authMode),
    policy.RequirePerm("project.write"),
)
```

### Tenant-scoped route

```go
r.Handle(http.MethodGet, "/api/v1/tenants/{tenant_id}/projects", handler,
    policy.AuthRequired(m.authEngine, auth.ModeStrict),
    policy.TenantRequired(),
    policy.TenantMatchFromPath("tenant_id"),
)
```

### Optional auth (public with context)

There is no built-in "optional auth" policy. If you need endpoints that work for both authenticated and anonymous users, check the auth context in the handler:

```go
func (h *Handler) PublicList(w http.ResponseWriter, r *http.Request) {
    principal, authenticated := auth.FromContext(r.Context())
    if authenticated {
        // Show user-specific data
    } else {
        // Show public data
    }
}
```

For this pattern, do NOT add `AuthRequired` — let the handler decide.

---

## Local development

### Auth disabled (simplest)

```
AUTH_ENABLED=false
```

No authentication is performed. All `AuthRequired` policies will return 401. Do not put `AuthRequired` on routes you want to test without auth.

### Auth enabled locally

```
AUTH_ENABLED=true
AUTH_MODE=jwt_only
REDIS_ENABLED=true
REDIS_ADDR=localhost:6379
POSTGRES_ENABLED=true
POSTGRES_URL=postgres://user:pass@localhost:5432/mydb?sslmode=disable
```

Use `jwt_only` mode locally to reduce strict session coupling during request validation. In this template, auth still requires Redis + Postgres to be enabled at startup.

### Generating test tokens

For integration tests, create a goAuth engine in test setup and mint tokens through the normal login flow:

```go
accessToken, refreshToken, err := engine.Login(ctx, "test@example.com", "test-password")
if err != nil {
    t.Fatal(err)
}

_ = refreshToken // use for refresh-flow tests as needed
req.Header.Set("Authorization", "Bearer "+accessToken)
```

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| 401 on all requests | `AUTH_ENABLED=false` with `AuthRequired` policy | Enable auth or remove policy |
| 401 with valid token | Token was signed/issued with incompatible key material or claims | Regenerate token with the same goAuth configuration used by the running API |
| 401 in strict mode | Redis down | Switch to hybrid mode or fix Redis |
| 403 "tenant scope required" | Token has no `tenant_id` | Ensure user is assigned to a tenant |
| 403 "forbidden" | Missing role or permission | Check token claims and route policy requirements |
| 404 on tenant routes | Tenant ID mismatch in path | User's tenant doesn't match URL tenant_id |
| Empty `AuthContext` | Token valid but claims missing | Check goAuth token generation includes all claims |
When auth is enabled in this template, both Redis and Postgres must also be enabled because the runtime wires a SQL-backed user provider for goAuth.
