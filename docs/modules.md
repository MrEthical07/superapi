# Module Author Guide

This guide explains how to build a module from scratch, what each file is for, and how to register routes properly.

---

## 1. Module folder structure

Every module lives under `internal/modules/<package_name>/` and follows this layout:

```
internal/modules/projects/
  module.go         # Module struct, Name(), BindDependencies()
  routes.go         # Register() — all route registrations + policy attachment
  dto.go            # Request/response types, Validate() methods, parsing helpers
  handler.go        # Handler struct, HTTP handler methods
  service.go        # Service interface + implementation, business logic, tx boundaries
  repo.go           # Repository interface + implementation, data access via sqlcgen.Queries
  db/
    schema.sql      # Module-local sqlc schema source (optional)
    queries.sql     # Module-local sqlc query source (optional)
  handler_test.go   # Handler unit tests
  service_test.go   # Service unit tests
  dto_test.go       # DTO validation tests (optional)
```

### What each file is for

| File | Responsibility | Rules |
|---|---|---|
| `module.go` | Declares the module struct, satisfies `app.Module` and optionally `app.DependencyBinder`. Stores injected dependencies. | Must export `New() *Module` and `Name() string`. |
| `routes.go` | Contains `Register(r httpx.Router) error`. Registers all HTTP routes with their handlers and policy stacks. | Only route wiring here. No business logic. No request handling. |
| `dto.go` | Request/response structs with JSON tags. Validation methods. Parsing helpers (e.g., `parseListLimit`). | DTOs implement `Validate() error` using typed `AppError`. |
| `handler.go` | Handler struct holding a reference to the service. Methods are HTTP handlers (`func(w, r)`) or typed JSON functions (`func(ctx, req) (resp, err)`). | Decodes input, calls service, writes response. No business logic. |
| `service.go` | Service interface + concrete implementation. Business logic, validation beyond DTO, transaction orchestration. | Owns transaction boundaries. Calls repo methods. |
| `repo.go` | Repository interface + concrete implementation. Wraps `*sqlcgen.Queries` methods. Maps DB rows to domain types. Maps DB errors to `AppError`. | No business logic. No transactions. |

---

## 2. Module interface

File: `internal/core/app/app.go`

```go
type Module interface {
    Name() string
    Register(r httpx.Router) error
}
```

Optional — implement `DependencyBinder` to receive infrastructure:

```go
type DependencyBinder interface {
    BindDependencies(*Dependencies)
}
```

`BindDependencies` is called before `Register`, so you can set up your handler/service/repo chain using injected deps.

---

## 3. Module registration

File: `internal/modules/modules.go`

```go
func All() []app.Module {
    return []app.Module{
        health.New(),
        system.New(),
        projects.New(),   // <-- add your module here
        // MODULE_LIST
    }
}
```

The `// MODULE_IMPORTS` and `// MODULE_LIST` markers are used by the module generator for idempotent insertion. Keep them in place.

---

## 4. Step-by-step: Building a `projects` module

### 4.1 Scaffold

```bash
make module name=projects
```

Or run `make module` with no `name` to use the interactive wizard. Create files manually only if you want a custom layout.

Advanced examples:

```bash
# Add module-local SQL stubs
make module name=projects db=1

# Pre-wire policy examples in routes
make module name=projects auth=1 tenant=1 ratelimit=1 cache=1

# Also scaffold a global migration (requires db=1)
make module name=projects db=1 migration=1
```

Constraints enforced by generator:

- `tenant=1` requires `auth=1`
- `migration=1` requires `db=1`

### 4.2 Define module.go

```go
package projects

import (
    "github.com/MrEthical07/superapi/internal/core/app"
    "github.com/MrEthical07/superapi/internal/core/auth"
    "github.com/MrEthical07/superapi/internal/core/cache"
    coredb "github.com/MrEthical07/superapi/internal/core/db"
    "github.com/MrEthical07/superapi/internal/core/ratelimit"
    "github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
    pool    *pgxpool.Pool
    handler *Handler
    cache   *cache.Manager
    auth    auth.Provider
    mode    auth.Mode
    limiter ratelimit.Limiter
    rlCfg   app.Dependencies // store what you need
}

func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

func (m *Module) Name() string { return "projects" }

func (m *Module) BindDependencies(deps *app.Dependencies) {
    if deps != nil {
        m.cache = deps.CacheMgr
        m.auth = deps.Auth
        m.mode = deps.AuthMode
        m.limiter = deps.Limiter
    }

    if deps == nil || deps.Postgres == nil {
        m.pool = nil
        m.handler = NewHandler(nil)
        return
    }

    m.pool = deps.Postgres
    repo := NewRepository(coredb.NewQueries(m.pool))
    svc := NewService(m.pool, repo)
    m.handler = NewHandler(svc)
}
```

Key points:
- Store injected dependencies as module fields
- Build the handler/service/repo chain in `BindDependencies`
- Handle the nil-pool case (Postgres disabled)

### 4.3 Define routes.go

```go
package projects

import (
    "net/http"
    "time"

    "github.com/MrEthical07/superapi/internal/core/cache"
    "github.com/MrEthical07/superapi/internal/core/httpx"
    "github.com/MrEthical07/superapi/internal/core/policy"
    "github.com/MrEthical07/superapi/internal/core/ratelimit"
)

func (m *Module) Register(r httpx.Router) error {
    if m.handler == nil {
        m.handler = NewHandler(nil)
    }

    authPol := policy.AuthRequired(m.auth, m.mode)

    // Create
    r.Handle(http.MethodPost, "/api/v1/projects", m.handler.Create(),
        authPol,
        policy.TenantRequired(),
        policy.CacheInvalidate(m.cache, cache.CacheInvalidateConfig{Tags: []string{"project"}}),
    )

    // Get by ID
    r.Handle(http.MethodGet, "/api/v1/projects/{id}", http.HandlerFunc(m.handler.GetByID),
        authPol,
        policy.TenantRequired(),
        policy.CacheRead(m.cache, cache.CacheReadConfig{
            TTL:                30 * time.Second,
            Tags:               []string{"project"},
            AllowAuthenticated: true,
            VaryBy: cache.CacheVaryBy{
                TenantID:   true,
                PathParams: []string{"id"},
            },
        }),
    )

    // List
    r.Handle(http.MethodGet, "/api/v1/projects", http.HandlerFunc(m.handler.List),
        authPol,
        policy.TenantRequired(),
        policy.CacheRead(m.cache, cache.CacheReadConfig{
            TTL:                15 * time.Second,
            Tags:               []string{"project"},
            AllowAuthenticated: true,
            VaryBy: cache.CacheVaryBy{
                TenantID:    true,
                QueryParams: []string{"limit"},
            },
        }),
    )

    // Update
    r.Handle(http.MethodPatch, "/api/v1/projects/{id}", m.handler.Update(),
        authPol,
        policy.TenantRequired(),
        policy.CacheInvalidate(m.cache, cache.CacheInvalidateConfig{Tags: []string{"project"}}),
    )

    // Delete
    r.Handle(http.MethodDelete, "/api/v1/projects/{id}", http.HandlerFunc(m.handler.Delete),
        authPol,
        policy.TenantRequired(),
        policy.RequirePerm("project.delete"),
        policy.CacheInvalidate(m.cache, cache.CacheInvalidateConfig{Tags: []string{"project"}}),
    )

    return nil
}
```

### 4.4 Define dto.go

```go
package projects

import (
    "strings"
    "time"

    apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

type createProjectRequest struct {
    Name string `json:"name"`
}

func (r createProjectRequest) Validate() error {
    if strings.TrimSpace(r.Name) == "" {
        return apperr.New(apperr.CodeBadRequest, 400, "name is required")
    }
    if len(r.Name) > 200 {
        return apperr.New(apperr.CodeBadRequest, 400, "name must be <= 200 chars")
    }
    return nil
}

type updateProjectRequest struct {
    Name   *string `json:"name"`
    Status *string `json:"status"`
}

func (r updateProjectRequest) Validate() error {
    if r.Name != nil && strings.TrimSpace(*r.Name) == "" {
        return apperr.New(apperr.CodeBadRequest, 400, "name cannot be blank")
    }
    return nil
}

type projectResponse struct {
    ID        string    `json:"id"`
    TenantID  string    `json:"tenant_id"`
    Name      string    `json:"name"`
    Status    string    `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type listProjectsResponse struct {
    Items []projectResponse `json:"items"`
    Count int               `json:"count"`
    Limit int               `json:"limit"`
}
```

### 4.5 Define handler.go

```go
package projects

import (
    "context"
    "net/http"
    "strings"

    apperr "github.com/MrEthical07/superapi/internal/core/errors"
    "github.com/MrEthical07/superapi/internal/core/httpx"
    "github.com/MrEthical07/superapi/internal/core/response"
    "github.com/MrEthical07/superapi/internal/core/tenant"
)

type Handler struct {
    svc Service
}

func NewHandler(svc Service) *Handler {
    return &Handler{svc: svc}
}

// Create returns an http.Handler using the typed JSON adapter.
func (h *Handler) Create() http.Handler {
    return httpx.JSON(h.create)
}

func (h *Handler) create(ctx context.Context, req createProjectRequest) (projectResponse, error) {
    if h.svc == nil {
        return projectResponse{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
    }
    tenantID, ok := tenant.TenantIDFromContext(ctx)
    if !ok {
        return projectResponse{}, apperr.New(apperr.CodeForbidden, 403, "tenant scope required")
    }
    p, err := h.svc.Create(ctx, tenantID, req)
    if err != nil {
        return projectResponse{}, err
    }
    return toProjectResponse(p), nil
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
    if h.svc == nil {
        response.Error(w, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured"), httpx.RequestIDFromContext(r.Context()))
        return
    }
    id := strings.TrimSpace(httpx.URLParam(r, "id"))
    if id == "" {
        response.Error(w, apperr.New(apperr.CodeBadRequest, 400, "id is required"), httpx.RequestIDFromContext(r.Context()))
        return
    }
    p, err := h.svc.GetByID(r.Context(), id)
    if err != nil {
        response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
        return
    }
    response.OK(w, toProjectResponse(p), httpx.RequestIDFromContext(r.Context()))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
    // ... parse limit, call svc.List, write response
}

// Update returns an http.Handler using the typed JSON adapter.
func (h *Handler) Update() http.Handler {
    return httpx.JSON(h.update)
}

func (h *Handler) update(ctx context.Context, req updateProjectRequest) (projectResponse, error) {
    // ... get id from context/params, call svc.Update
    return projectResponse{}, nil
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
    // ... parse id, call svc.Delete, write response
}
```

**Two handler patterns:**

1. **Typed JSON handler** (`httpx.JSON(fn)`) — for POST/PUT/PATCH with JSON bodies. Returns `http.Handler`.
2. **Classic handler** (`func(w, r)`) — for GET/DELETE or when you need direct `http.Request` access. Wrap with `http.HandlerFunc(m.handler.Method)`.

### 4.6 Define service.go

```go
package projects

import (
    "context"

    coredb "github.com/MrEthical07/superapi/internal/core/db"
    "github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
    apperr "github.com/MrEthical07/superapi/internal/core/errors"
    "github.com/jackc/pgx/v5/pgxpool"
)

type Service interface {
    Create(ctx context.Context, tenantID string, req createProjectRequest) (Project, error)
    GetByID(ctx context.Context, id string) (Project, error)
    List(ctx context.Context, tenantID string, limit int32) ([]Project, error)
    Update(ctx context.Context, id string, req updateProjectRequest) (Project, error)
    Delete(ctx context.Context, id string) error
}

type service struct {
    pool *pgxpool.Pool
    repo Repository
}

func NewService(pool *pgxpool.Pool, repo Repository) Service {
    if pool == nil || repo == nil {
        return &service{} // nil service — handlers check and return 503
    }
    return &service{pool: pool, repo: repo}
}

// Write path: use transaction
func (s *service) Create(ctx context.Context, tenantID string, req createProjectRequest) (Project, error) {
    if s.pool == nil {
        return Project{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
    }
    return coredb.WithTxResult(ctx, s.pool, func(q *sqlcgen.Queries) (Project, error) {
        return NewRepository(q).Create(ctx, CreateProjectInput{
            TenantID: tenantID,
            Name:     req.Name,
        })
    })
}

// Read path: use pool directly (no transaction needed)
func (s *service) GetByID(ctx context.Context, id string) (Project, error) {
    if s.repo == nil {
        return Project{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
    }
    return s.repo.GetByID(ctx, id)
}

// ... List, Update, Delete follow the same patterns
```

### 4.7 Define repo.go

```go
package projects

import (
    "context"
    "errors"
    "time"

    "github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
    apperr "github.com/MrEthical07/superapi/internal/core/errors"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
)

type Project struct {
    ID        string
    TenantID  string
    Name      string
    Status    string
    CreatedAt time.Time
    UpdatedAt time.Time
}

type Repository interface {
    Create(ctx context.Context, input CreateProjectInput) (Project, error)
    GetByID(ctx context.Context, id string) (Project, error)
    List(ctx context.Context, tenantID string, limit int32) ([]Project, error)
    Delete(ctx context.Context, id string) error
}

type repository struct {
    q *sqlcgen.Queries
}

func NewRepository(q *sqlcgen.Queries) Repository {
    return &repository{q: q}
}

func (r *repository) Create(ctx context.Context, input CreateProjectInput) (Project, error) {
    row, err := r.q.CreateProject(ctx, sqlcgen.CreateProjectParams{
        ID:       input.ID,
        TenantID: input.TenantID,
        Name:     input.Name,
        Status:   "active",
    })
    if err != nil {
        var pgErr *pgconn.PgError
        if errors.As(err, &pgErr) && pgErr.Code == "23505" {
            return Project{}, apperr.New(apperr.CodeConflict, 409, "project already exists")
        }
        return Project{}, err
    }
    return fromRow(row), nil
}

func (r *repository) GetByID(ctx context.Context, id string) (Project, error) {
    row, err := r.q.GetProjectByID(ctx, id)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return Project{}, apperr.New(apperr.CodeNotFound, 404, "project not found")
        }
        return Project{}, err
    }
    return fromRow(row), nil
}

// ... List, Delete follow the same patterns
```

---

## 5. Registering routes properly

### Router interface

```go
type Router interface {
    Handle(method string, pattern string, h http.Handler, policies ...policy.Policy)
    Use(middlewares ...func(http.Handler) http.Handler)
}
```

### Route registration rules

1. Call `r.Handle(method, pattern, handler, policies...)` for each endpoint
2. Policies are variadic — pass zero or many
3. Policy order matters: first listed = outermost (runs first on request, last on response)
4. Pattern uses chi syntax: `/api/v1/projects/{id}` for path params
5. Handler must be `http.Handler` — use `http.HandlerFunc(fn)` for plain functions or `httpx.JSON(fn)` for typed JSON

### Policy attachment order convention

For a typical authenticated, tenant-scoped endpoint:

1. `policy.AuthRequired(provider, mode)` — must be first (outermost)
2. `policy.TenantRequired()` — after auth
3. `policy.TenantMatchFromPath("tenant_id")` — if tenant_id is in the URL path
4. `policy.RequireRole(...)` / `policy.RequirePerm(...)` — RBAC after tenant check
5. `policy.RateLimit(limiter, rule)` — after auth (so user scope is available for keying)
6. `policy.CacheRead(mgr, cfg)` — for GET endpoints (innermost, closest to handler)
7. `policy.CacheInvalidate(mgr, cfg)` — for write endpoints

---

## 6. Using the typed JSON adapter

For endpoints with JSON request bodies, use `httpx.JSON`:

```go
func (h *Handler) Create() http.Handler {
    return httpx.JSON(h.create)
}

func (h *Handler) create(ctx context.Context, req createProjectRequest) (projectResponse, error) {
    // Business logic via service
    result, err := h.svc.Create(ctx, req)
    if err != nil {
        return projectResponse{}, err  // AppError passed through; others become 500
    }
    return toProjectResponse(result), nil
}
```

The adapter handles:
- Body size limit (1 MiB)
- Strict JSON decode (unknown fields rejected)
- Validation (`Validate()` called automatically if implemented)
- Error mapping (AppError → correct HTTP status; other errors → 500)
- Response envelope (`response.OK()`)

### When NOT to use typed JSON

- GET/DELETE endpoints (no request body) — use classic `func(w, r)` handlers
- Endpoints needing direct access to `http.Request` (e.g., reading path params, headers, query params)
- Streaming or non-JSON responses

---

## 7. Common mistakes

### Mistake: Business logic in handler

```go
// BAD — handler does too much
func (h *Handler) Create(ctx context.Context, req createProjectRequest) (projectResponse, error) {
    if req.Status == "archived" && !isAdmin(ctx) {
        return projectResponse{}, errors.New(...)
    }
    // ... direct DB call ...
}
```

```go
// GOOD — handler delegates to service
func (h *Handler) Create(ctx context.Context, req createProjectRequest) (projectResponse, error) {
    result, err := h.svc.Create(ctx, tenantID, req)
    if err != nil {
        return projectResponse{}, err
    }
    return toProjectResponse(result), nil
}
```

### Mistake: Repository starts a transaction

```go
// BAD — repo should not manage tx
func (r *repository) Create(ctx context.Context, input Input) (Project, error) {
    tx, _ := r.pool.Begin(ctx)  // WRONG
    // ...
}
```

```go
// GOOD — service owns tx boundary
func (s *service) Create(ctx context.Context, req Request) (Project, error) {
    return db.WithTxResult(ctx, s.pool, func(q *sqlcgen.Queries) (Project, error) {
        return NewRepository(q).Create(ctx, input)
    })
}
```

### Mistake: Returning raw errors to clients

```go
// BAD — leaks internal details
return fmt.Errorf("pg error: %w", err)
```

```go
// GOOD — use AppError for known cases; let unknown errors hit the 500 sanitizer
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) && pgErr.Code == "23505" {
    return apperr.New(apperr.CodeConflict, 409, "resource already exists")
}
return err  // unknown error → response.Error() maps to 500 "internal server error"
```

### Mistake: Forgetting nil checks for optional dependencies

```go
// BAD — panics when Postgres disabled
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
    items, err := h.svc.List(r.Context(), 50)
    // ...
}

// GOOD — check first
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
    if h.svc == nil {
        response.Error(w, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured"), ...)
        return
    }
    items, err := h.svc.List(r.Context(), 50)
    // ...
}
```
