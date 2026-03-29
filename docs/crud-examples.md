# CRUD Examples

End-to-end walkthrough of building a CRUD module in SuperAPI. Uses a fictional **projects** module as the example, showing every file from database migration to route registration.

> **Reference note:** There is no built-in tenant CRUD module registered by default; this document is the canonical CRUD module pattern.

---

## 1. Database setup

### Migration

Create `db/migrations/000003_projects.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS projects (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS projects_tenant_id_idx ON projects (tenant_id);
CREATE INDEX IF NOT EXISTS projects_created_at_idx ON projects (created_at);
```

Create `db/migrations/000003_projects.down.sql`:

```sql
DROP TABLE IF EXISTS projects;
```

### Schema mirror

Copy the `up.sql` content (minus the `IF NOT EXISTS`) to `db/schema/projects.sql`:

```sql
CREATE TABLE IF NOT EXISTS projects (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS projects_tenant_id_idx ON projects (tenant_id);
CREATE INDEX IF NOT EXISTS projects_created_at_idx ON projects (created_at);
```

### Queries

Create `db/queries/projects.sql`:

```sql
-- name: CreateProject :one
INSERT INTO projects (id, tenant_id, name, status)
VALUES ($1, $2, $3, $4)
RETURNING id, tenant_id, name, status, created_at, updated_at;

-- name: GetProjectByID :one
SELECT id, tenant_id, name, status, created_at, updated_at
FROM projects
WHERE id = $1 AND tenant_id = $2;

-- name: ListProjectsByTenant :many
SELECT id, tenant_id, name, status, created_at, updated_at
FROM projects
WHERE tenant_id = $1
ORDER BY created_at DESC, id ASC
LIMIT $2;

-- name: UpdateProject :one
UPDATE projects
SET name = $3, status = $4, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2
RETURNING id, tenant_id, name, status, created_at, updated_at;

-- name: DeleteProject :exec
DELETE FROM projects
WHERE id = $1 AND tenant_id = $2;
```

### Generate sqlc

```bash
make sqlc-generate
```

This writes Go types and query functions to `internal/core/db/sqlcgen/`.

### Run migration

```bash
make migrate-up
```

---

## 2. Module scaffolding

Use the generator or create files manually. The file structure:

```
internal/modules/projects/
├── module.go     # Module struct, Name(), BindDependencies()
├── routes.go     # Register() with policies
├── dto.go        # Request/response types, Validate()
├── handler.go    # HTTP handlers
├── service.go    # Business logic, transactions
└── repo.go       # Repository over sqlcgen
```

### module.go

```go
package projects

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	pool     *pgxpool.Pool
	auth     auth.Provider
	mode     auth.Mode
	limiter  *ratelimit.RedisLimiter
	cacheMgr *cache.Manager
	handler  *Handler
}

func New() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "projects"
}

func (m *Module) BindDependencies(d *app.Dependencies) {
	m.pool = d.Postgres
	m.auth = d.Auth
	m.mode = d.AuthMode
	m.limiter = d.Limiter
	m.cacheMgr = d.CacheMgr

	if m.pool != nil {
		repo := NewRepository(d.Queries)
		svc := NewService(m.pool, repo)
		m.handler = NewHandler(svc)
	}
}
```

**Key patterns:**
- Dependencies are stored as fields, not globals
- Handler is created from service, service from repo — constructor chain
- Nil-safe: if Postgres is disabled, handler is nil and routes can check for it

---

### dto.go

```go
package projects

import (
	"strconv"
	"strings"
	"time"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

const (
	statusActive   = "active"
	statusArchived = "archived"

	defaultListLimit = 50
	maxListLimit     = 100
)

// --- Create ---

type createProjectRequest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (r createProjectRequest) Validate() error {
	name := strings.TrimSpace(r.Name)
	status := strings.TrimSpace(strings.ToLower(r.Status))

	if name == "" {
		return apperr.New(apperr.CodeBadRequest, 400, "name is required")
	}
	if len(name) > 120 {
		return apperr.New(apperr.CodeBadRequest, 400, "name must be <= 120 chars")
	}
	if status != statusActive && status != statusArchived {
		return apperr.New(apperr.CodeBadRequest, 400, "status must be one of: active, archived")
	}
	return nil
}

// --- Update ---

type updateProjectRequest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (r updateProjectRequest) Validate() error {
	name := strings.TrimSpace(r.Name)
	status := strings.TrimSpace(strings.ToLower(r.Status))

	if name == "" {
		return apperr.New(apperr.CodeBadRequest, 400, "name is required")
	}
	if len(name) > 120 {
		return apperr.New(apperr.CodeBadRequest, 400, "name must be <= 120 chars")
	}
	if status != statusActive && status != statusArchived {
		return apperr.New(apperr.CodeBadRequest, 400, "status must be one of: active, archived")
	}
	return nil
}

// --- Responses ---

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

// --- Helpers ---

func parseListLimit(raw string) (int32, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultListLimit, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, apperr.New(apperr.CodeBadRequest, 400, "limit must be a valid integer")
	}
	if limit <= 0 || limit > maxListLimit {
		return 0, apperr.New(apperr.CodeBadRequest, 400, "limit must be between 1 and 100")
	}
	return int32(limit), nil
}
```

**Key patterns:**
- Request types have a `Validate() error` method — used by `httpx.JSON` automatically
- Use `apperr.New()` for validation errors with explicit HTTP status code
- Response types map 1:1 to API JSON output
- Parsing helpers for query params return `AppError` for consistent error responses

---

### handler.go

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

// --- POST /api/v1/projects (typed JSON handler) ---

func (h *Handler) Create() http.Handler {
	return httpx.JSON(h.create)
}

func (h *Handler) create(ctx context.Context, req createProjectRequest) (projectResponse, error) {
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

// --- GET /api/v1/projects/{id} (manual handler) ---

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant.TenantIDFromContext(r.Context())
	if !ok {
		response.Error(w, apperr.New(apperr.CodeForbidden, 403, "tenant scope required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	id := strings.TrimSpace(httpx.URLParam(r, "id"))
	if id == "" {
		response.Error(w, apperr.New(apperr.CodeBadRequest, 400, "id is required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	p, err := h.svc.GetByID(r.Context(), tenantID, id)
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}
	response.OK(w, toProjectResponse(p), httpx.RequestIDFromContext(r.Context()))
}

// --- GET /api/v1/projects (manual handler) ---

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant.TenantIDFromContext(r.Context())
	if !ok {
		response.Error(w, apperr.New(apperr.CodeForbidden, 403, "tenant scope required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	limit, err := parseListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}

	items, err := h.svc.List(r.Context(), tenantID, limit)
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}

	out := make([]projectResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toProjectResponse(item))
	}

	response.OK(w, listProjectsResponse{
		Items: out,
		Count: len(out),
		Limit: int(limit),
	}, httpx.RequestIDFromContext(r.Context()))
}

// --- PUT /api/v1/projects/{id} (typed JSON handler) ---

func (h *Handler) Update() http.Handler {
	return httpx.JSON(h.update)
}

func (h *Handler) update(ctx context.Context, req updateProjectRequest) (projectResponse, error) {
	tenantID, ok := tenant.TenantIDFromContext(ctx)
	if !ok {
		return projectResponse{}, apperr.New(apperr.CodeForbidden, 403, "tenant scope required")
	}

	// httpx.JSON doesn't expose URL params in ctx, so we use CaptureRoutePattern
	// For typed JSON handlers that need path params, use manual handler pattern instead.
	// This is a simplified example — see note below.
	return projectResponse{}, apperr.New(apperr.CodeBadRequest, 400, "use manual handler for path param routes")
}

// Better: manual handler for update (needs path param)
func (h *Handler) UpdateManual(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant.TenantIDFromContext(r.Context())
	if !ok {
		response.Error(w, apperr.New(apperr.CodeForbidden, 403, "tenant scope required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	id := strings.TrimSpace(httpx.URLParam(r, "id"))
	if id == "" {
		response.Error(w, apperr.New(apperr.CodeBadRequest, 400, "id is required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	var req updateProjectRequest
	if err := httpx.DecodeAndValidateJSON(w, r, &req); err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}

	p, err := h.svc.Update(r.Context(), tenantID, id, req)
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}
	response.OK(w, toProjectResponse(p), httpx.RequestIDFromContext(r.Context()))
}

// --- DELETE /api/v1/projects/{id} (manual handler) ---

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant.TenantIDFromContext(r.Context())
	if !ok {
		response.Error(w, apperr.New(apperr.CodeForbidden, 403, "tenant scope required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	id := strings.TrimSpace(httpx.URLParam(r, "id"))
	if id == "" {
		response.Error(w, apperr.New(apperr.CodeBadRequest, 400, "id is required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	if err := h.svc.Delete(r.Context(), tenantID, id); err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}
	response.OK(w, nil, httpx.RequestIDFromContext(r.Context()))
}

// --- Mapper ---

func toProjectResponse(p Project) projectResponse {
	return projectResponse{
		ID:        p.ID,
		TenantID:  p.TenantID,
		Name:      p.Name,
		Status:    p.Status,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}
```

**Two handler styles:**

| Style | When to use | Example |
|---|---|---|
| **Typed JSON** (`httpx.JSON`) | POST/PUT with JSON body, no path params needed | `Create()` |
| **Manual** (`http.HandlerFunc`) | Routes with path params, query params, or custom response shapes | `GetByID`, `List`, `Delete`, `UpdateManual` |

> **Note on typed JSON + path params:** `httpx.JSON` receives only the decoded body. It does not have access to `http.Request` directly, so you cannot read URL params. For endpoints that need both a JSON body and path params (like update), use a manual handler.

---

### service.go

```go
package projects

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	coredb "github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service interface {
	Create(ctx context.Context, tenantID string, req createProjectRequest) (Project, error)
	GetByID(ctx context.Context, tenantID, id string) (Project, error)
	List(ctx context.Context, tenantID string, limit int32) ([]Project, error)
	Update(ctx context.Context, tenantID, id string, req updateProjectRequest) (Project, error)
	Delete(ctx context.Context, tenantID, id string) error
}

type service struct {
	pool *pgxpool.Pool
	repo Repository
}

func NewService(pool *pgxpool.Pool, repo Repository) Service {
	if pool == nil || repo == nil {
		return &service{}
	}
	return &service{pool: pool, repo: repo}
}

// Create runs inside a transaction (write path)
func (s *service) Create(ctx context.Context, tenantID string, req createProjectRequest) (Project, error) {
	if s.pool == nil {
		return Project{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}

	input := CreateProjectInput{
		ID:       newProjectID(),
		TenantID: tenantID,
		Name:     strings.TrimSpace(req.Name),
		Status:   strings.TrimSpace(strings.ToLower(req.Status)),
	}

	return coredb.WithTxResult(ctx, s.pool, func(q *sqlcgen.Queries) (Project, error) {
		return s.repo.CreateTx(ctx, NewRepository(q), input)
	})
}

// GetByID reads outside a transaction (read path)
func (s *service) GetByID(ctx context.Context, tenantID, id string) (Project, error) {
	if s.repo == nil {
		return Project{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}
	return s.repo.GetByID(ctx, tenantID, id)
}

// List reads outside a transaction (read path)
func (s *service) List(ctx context.Context, tenantID string, limit int32) ([]Project, error) {
	if s.repo == nil {
		return nil, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}
	return s.repo.List(ctx, tenantID, limit)
}

// Update runs inside a transaction (write path)
func (s *service) Update(ctx context.Context, tenantID, id string, req updateProjectRequest) (Project, error) {
	if s.pool == nil {
		return Project{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}

	return coredb.WithTxResult(ctx, s.pool, func(q *sqlcgen.Queries) (Project, error) {
		return NewRepository(q).Update(ctx, tenantID, id, UpdateProjectInput{
			Name:   strings.TrimSpace(req.Name),
			Status: strings.TrimSpace(strings.ToLower(req.Status)),
		})
	})
}

// Delete runs inside a transaction (write path)
func (s *service) Delete(ctx context.Context, tenantID, id string) error {
	if s.pool == nil {
		return apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}

	return coredb.WithTx(ctx, s.pool, func(q *sqlcgen.Queries) error {
		return NewRepository(q).Delete(ctx, tenantID, id)
	})
}

func newProjectID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "proj_00000000000000000000000000000000"
	}
	return "proj_" + hex.EncodeToString(b[:])
}
```

**Transaction boundaries:**
- **Reads** (`GetByID`, `List`) use the module-level repo directly — no transaction needed
- **Writes** (`Create`, `Update`, `Delete`) use `coredb.WithTxResult` or `coredb.WithTx` — the callback receives a `*sqlcgen.Queries` bound to the transaction
- Inside the callback, create a **new** Repository from the tx-bound queries: `NewRepository(q)`
- If the callback returns an error or panics, the transaction is rolled back automatically

---

### repo.go

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

type CreateProjectInput struct {
	ID       string
	TenantID string
	Name     string
	Status   string
}

type UpdateProjectInput struct {
	Name   string
	Status string
}

type Repository interface {
	CreateTx(ctx context.Context, txRepo Repository, input CreateProjectInput) (Project, error)
	GetByID(ctx context.Context, tenantID, id string) (Project, error)
	List(ctx context.Context, tenantID string, limit int32) ([]Project, error)
	Update(ctx context.Context, tenantID, id string, input UpdateProjectInput) (Project, error)
	Delete(ctx context.Context, tenantID, id string) error
}

type repository struct {
	q *sqlcgen.Queries
}

func NewRepository(q *sqlcgen.Queries) Repository {
	return &repository{q: q}
}

func (r *repository) CreateTx(ctx context.Context, txRepo Repository, input CreateProjectInput) (Project, error) {
	// txRepo is the repository bound to the transaction
	// Use txRepo for the actual insert
	return txRepo.(*repository).create(ctx, input)
}

func (r *repository) create(ctx context.Context, input CreateProjectInput) (Project, error) {
	row, err := r.q.CreateProject(ctx, sqlcgen.CreateProjectParams{
		ID:       input.ID,
		TenantID: input.TenantID,
		Name:     input.Name,
		Status:   input.Status,
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

func (r *repository) GetByID(ctx context.Context, tenantID, id string) (Project, error) {
	row, err := r.q.GetProjectByID(ctx, sqlcgen.GetProjectByIDParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, apperr.New(apperr.CodeNotFound, 404, "project not found")
		}
		return Project{}, err
	}
	return fromRow(row), nil
}

func (r *repository) List(ctx context.Context, tenantID string, limit int32) ([]Project, error) {
	rows, err := r.q.ListProjectsByTenant(ctx, sqlcgen.ListProjectsByTenantParams{
		TenantID: tenantID,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]Project, 0, len(rows))
	for _, row := range rows {
		items = append(items, fromRow(row))
	}
	return items, nil
}

func (r *repository) Update(ctx context.Context, tenantID, id string, input UpdateProjectInput) (Project, error) {
	row, err := r.q.UpdateProject(ctx, sqlcgen.UpdateProjectParams{
		ID:       id,
		TenantID: tenantID,
		Name:     input.Name,
		Status:   input.Status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, apperr.New(apperr.CodeNotFound, 404, "project not found")
		}
		return Project{}, err
	}
	return fromRow(row), nil
}

func (r *repository) Delete(ctx context.Context, tenantID, id string) error {
	// Note: pgx :exec doesn't return rows, so we can't detect not_found
	// If you need a 404 on missing delete, use :one with RETURNING
	err := r.q.DeleteProject(ctx, sqlcgen.DeleteProjectParams{
		ID:       id,
		TenantID: tenantID,
	})
	return err
}

func fromRow(row sqlcgen.Project) Project {
	return Project{
		ID:        row.ID,
		TenantID:  row.TenantID,
		Name:      row.Name,
		Status:    row.Status,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}
```

**Error mapping:**
- `pgx.ErrNoRows` → 404 `not_found`
- `pgconn.PgError` code `23505` (unique violation) → 409 `conflict`
- Other errors bubble up as 500 (sanitized by response.Error)

---

### routes.go

```go
package projects

import (
	"net/http"
	"time"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

func (m *Module) Register(r httpx.Router) error {
	if m.handler == nil {
		m.handler = NewHandler(nil)
	}

	rlRule := ratelimit.Rule{Limit: 60, Window: time.Minute}

	// POST /api/v1/projects — Create
	r.Handle(http.MethodPost, "/api/v1/projects", m.handler.Create(),
		policy.AuthRequired(m.auth, auth.ModeStrict),
		policy.TenantRequired(),
		policy.RequirePerm("project.write"),
		policy.RateLimitWithKeyer(m.limiter, "projects.create", rlRule, ratelimit.KeyByTenant()),
		policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
			Tags: []string{"project"},
		}),
	)

	// GET /api/v1/projects/{id} — Get by ID
	r.Handle(http.MethodGet, "/api/v1/projects/{id}", http.HandlerFunc(m.handler.GetByID),
		policy.AuthRequired(m.auth, m.mode),
		policy.TenantRequired(),
		policy.RateLimitWithKeyer(m.limiter, "projects.get", rlRule, ratelimit.KeyByTenant()),
		policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
			TTL:                30 * time.Second,
			Tags:               []string{"project"},
			AllowAuthenticated: true,
			VaryBy: cache.CacheVaryBy{
				TenantID:   true,
				PathParams: []string{"id"},
			},
		}),
	)

	// GET /api/v1/projects — List
	r.Handle(http.MethodGet, "/api/v1/projects", http.HandlerFunc(m.handler.List),
		policy.AuthRequired(m.auth, m.mode),
		policy.TenantRequired(),
		policy.RateLimitWithKeyer(m.limiter, "projects.list", rlRule, ratelimit.KeyByTenant()),
		policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
			TTL:                15 * time.Second,
			Tags:               []string{"project"},
			AllowAuthenticated: true,
			VaryBy: cache.CacheVaryBy{
				TenantID:    true,
				QueryParams: []string{"limit"},
			},
		}),
	)

	// PUT /api/v1/projects/{id} — Update
	r.Handle(http.MethodPut, "/api/v1/projects/{id}", http.HandlerFunc(m.handler.UpdateManual),
		policy.AuthRequired(m.auth, auth.ModeStrict),
		policy.TenantRequired(),
		policy.RequirePerm("project.write"),
		policy.RateLimitWithKeyer(m.limiter, "projects.update", rlRule, ratelimit.KeyByTenant()),
		policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
			Tags: []string{"project"},
		}),
	)

	// DELETE /api/v1/projects/{id} — Delete
	r.Handle(http.MethodDelete, "/api/v1/projects/{id}", http.HandlerFunc(m.handler.Delete),
		policy.AuthRequired(m.auth, auth.ModeStrict),
		policy.TenantRequired(),
		policy.RequirePerm("project.delete"),
		policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
			Tags: []string{"project"},
		}),
	)

	return nil
}
```

---

## 3. Register the module

Add to `internal/modules/modules.go`:

```go
import (
	// ...existing imports...
	"github.com/MrEthical07/superapi/internal/modules/projects"
)

func All() []app.Module {
	return []app.Module{
		health.New(),
		system.New(),
		projects.New(),  // Add here
	}
}
```

---

## 4. API request/response examples

### Create

```
POST /api/v1/projects
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "My Project",
  "status": "active"
}
```

Success (201 via httpx.JSON):

```json
{
  "ok": true,
  "data": {
    "id": "proj_a1b2c3d4e5f6...",
    "tenant_id": "tenant_xyz...",
    "name": "My Project",
    "status": "active",
    "created_at": "2025-01-15T10:30:00Z",
    "updated_at": "2025-01-15T10:30:00Z"
  },
  "request_id": "req_abc123"
}
```

Validation error (400):

```json
{
  "ok": false,
  "error": {
    "code": "bad_request",
    "message": "name is required"
  },
  "request_id": "req_abc123"
}
```

Conflict (409):

```json
{
  "ok": false,
  "error": {
    "code": "conflict",
    "message": "project already exists"
  },
  "request_id": "req_abc123"
}
```

### Get by ID

```
GET /api/v1/projects/proj_a1b2c3d4e5f6
Authorization: Bearer <token>
```

Success (200):

```json
{
  "ok": true,
  "data": {
    "id": "proj_a1b2c3d4e5f6...",
    "tenant_id": "tenant_xyz...",
    "name": "My Project",
    "status": "active",
    "created_at": "2025-01-15T10:30:00Z",
    "updated_at": "2025-01-15T10:30:00Z"
  },
  "request_id": "req_abc123"
}
```

Not found (404):

```json
{
  "ok": false,
  "error": {
    "code": "not_found",
    "message": "project not found"
  },
  "request_id": "req_abc123"
}
```

### List

```
GET /api/v1/projects?limit=20
Authorization: Bearer <token>
```

Success (200):

```json
{
  "ok": true,
  "data": {
    "items": [
      {
        "id": "proj_a1b2c3d4e5f6...",
        "tenant_id": "tenant_xyz...",
        "name": "My Project",
        "status": "active",
        "created_at": "2025-01-15T10:30:00Z",
        "updated_at": "2025-01-15T10:30:00Z"
      }
    ],
    "count": 1,
    "limit": 20
  },
  "request_id": "req_abc123"
}
```

### Update

```
PUT /api/v1/projects/proj_a1b2c3d4e5f6
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Renamed Project",
  "status": "archived"
}
```

Success (200):

```json
{
  "ok": true,
  "data": {
    "id": "proj_a1b2c3d4e5f6...",
    "tenant_id": "tenant_xyz...",
    "name": "Renamed Project",
    "status": "archived",
    "created_at": "2025-01-15T10:30:00Z",
    "updated_at": "2025-01-15T11:00:00Z"
  },
  "request_id": "req_abc123"
}
```

### Delete

```
DELETE /api/v1/projects/proj_a1b2c3d4e5f6
Authorization: Bearer <token>
```

Success (200):

```json
{
  "ok": true,
  "data": null,
  "request_id": "req_abc123"
}
```

---

## 5. Error mapping cheat sheet

| Source | HTTP status | Error code | Example |
|---|---|---|---|
| DTO `Validate()` | 400 | `bad_request` | Missing required field |
| `pgx.ErrNoRows` | 404 | `not_found` | Record doesn't exist |
| PgError `23505` | 409 | `conflict` | Unique constraint violation |
| Auth missing | 401 | `unauthorized` | No/invalid token |
| Wrong role/perm | 403 | `forbidden` | Insufficient privileges |
| No tenant | 403 | `forbidden` | Tenant scope required |
| Tenant mismatch | 404 | `not_found` | Accessing another tenant's data |
| Rate limited | 429 | `too_many_requests` | Rate limit exceeded |
| Context timeout | 504 | `timeout` | Request exceeded deadline |
| DB unavailable | 503 | `dependency_unavailable` | Postgres not configured |
| Unhandled error | 500 | `internal_error` | Sanitized — details stripped |

---

## 6. Summary checklist

When building a new CRUD module:

- [ ] Create migration (`db/migrations/`)
- [ ] Mirror schema (`db/schema/`)
- [ ] Write queries (`db/queries/`)
- [ ] Run `make sqlc-generate`
- [ ] Create module directory (`internal/modules/{name}/`)
- [ ] Write `module.go` — struct, Name(), BindDependencies()
- [ ] Write `dto.go` — request/response types, Validate()
- [ ] Write `repo.go` — domain type, Repository interface, sqlcgen wrapper, error mapping
- [ ] Write `service.go` — Service interface, business logic, tx boundaries
- [ ] Write `handler.go` — HTTP handlers (typed JSON for body-only, manual for path params)
- [ ] Write `routes.go` — Register() with policies in correct order
- [ ] Register module in `internal/modules/modules.go`
- [ ] Run `make migrate-up`
- [ ] Run `go test ./...`
