# CRUD Examples (Store-First)

This guide walks through a realistic CRUD module using the enforced architecture:

Service -> Repository -> Store -> Backend

The example module name is projects.

## 1. What You Are Building

Routes:

- POST /api/v1/projects
- GET /api/v1/projects/{id}
- GET /api/v1/projects
- PUT /api/v1/projects/{id}
- DELETE /api/v1/projects/{id}

Each route follows:

handler -> service -> repository -> store

## 2. Data Model

Domain model (repo/service-facing):

```go
type Project struct {
    ID       string
    TenantID string
    Name     string
    Status   string
}
```

Keep this model independent from database row structs and driver types.

## 3. Relational Setup (If Using Postgres)

If your module uses relational backend:

1. Add migration files under db/migrations.
2. Mirror schema shape under db/schema.
3. Add query definitions under db/queries.
4. Regenerate sqlc output if you use sqlc-based internals.

Example migration (up):

```sql
CREATE TABLE IF NOT EXISTS projects (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS projects_tenant_id_idx ON projects (tenant_id);
```

Note:

- this SQL exists in storage layer internals
- do not leak sqlc query objects into service/repository public contracts

## 4. Repository Interface (Domain Contract)

Define repository interface in domain terms.

```go
type CreateProjectInput struct {
    ID       string
    TenantID string
    Name     string
    Status   string
}

type ProjectRepository interface {
    Create(ctx context.Context, input CreateProjectInput) (Project, error)
    GetByID(ctx context.Context, tenantID, id string) (Project, error)
    List(ctx context.Context, tenantID string, limit int32) ([]Project, error)
    Update(ctx context.Context, tenantID, id string, name string, status string) (Project, error)
    Delete(ctx context.Context, tenantID, id string) error
}
```

Do not expose backend-specific types in this contract.

## 5. Service Layer (Workflow + Transactions)

Service calls repository methods and chooses transaction boundary for writes.

```go
type service struct {
    repo  ProjectRepository
    store storage.RelationalStore
}

func (s *service) Create(ctx context.Context, tenantID string, req createProjectRequest) (Project, error) {
    input := CreateProjectInput{
        ID:       newProjectID(),
        TenantID: tenantID,
        Name:     strings.TrimSpace(req.Name),
        Status:   strings.TrimSpace(strings.ToLower(req.Status)),
    }

    var out Project
    err := s.store.WithTx(ctx, func(txCtx context.Context) error {
        created, err := s.repo.Create(txCtx, input)
        if err != nil {
            return err
        }
        out = created
        return nil
    })
    return out, err
}

func (s *service) GetByID(ctx context.Context, tenantID, id string) (Project, error) {
    return s.repo.GetByID(ctx, tenantID, id)
}
```

Rules shown above:

- write path uses store.WithTx
- read path is direct repository call
- service does not construct SQL

## 6. Relational Repository Implementation

Repository owns query and mapping logic.

```go
type relationalRepository struct {
    store storage.RelationalStore
}

func (r *relationalRepository) Create(ctx context.Context, input CreateProjectInput) (Project, error) {
    var out Project
    err := r.store.Execute(ctx, storage.RelationalQueryOne(`
INSERT INTO projects (id, tenant_id, name, status)
VALUES ($1, $2, $3, $4)
RETURNING id, tenant_id, name, status
`, func(row storage.RowScanner) error {
        return row.Scan(&out.ID, &out.TenantID, &out.Name, &out.Status)
    }, input.ID, input.TenantID, input.Name, input.Status))
    if err != nil {
        return Project{}, err
    }
    return out, nil
}

func (r *relationalRepository) GetByID(ctx context.Context, tenantID, id string) (Project, error) {
    var out Project
    err := r.store.Execute(ctx, storage.RelationalQueryOne(`
SELECT id, tenant_id, name, status
FROM projects
WHERE tenant_id = $1 AND id = $2
`, func(row storage.RowScanner) error {
        return row.Scan(&out.ID, &out.TenantID, &out.Name, &out.Status)
    }, tenantID, id))
    if err != nil {
        return Project{}, err
    }
    return out, nil
}
```

This keeps store execution-only while repository remains domain-aware.

## 7. Document Repository Variant

For document-backed modules, keep same repository contract and change implementation only.

```go
type documentRepository struct {
    store storage.DocumentStore
}

func (r *documentRepository) Create(ctx context.Context, input CreateProjectInput) (Project, error) {
    payload := map[string]any{
        "id":        input.ID,
        "tenant_id": input.TenantID,
        "name":      input.Name,
        "status":    input.Status,
    }

    var out Project
    err := r.store.Execute(ctx, storage.DocumentRun("projects.insert", payload, &out))
    if err != nil {
        return Project{}, err
    }
    return out, nil
}
```

Do not branch SQL/document behavior in one module implementation.

## 8. Handler Layer Example

Handler reads transport input and delegates to service.

```go
func (h *Handler) Create(ctx *httpx.Context, req createProjectRequest) (projectResponse, error) {
    tenantID, ok := tenant.TenantIDFromContext(ctx.Context())
    if !ok {
        return projectResponse{}, apperr.New(apperr.CodeForbidden, 403, "tenant scope required")
    }

    project, err := h.svc.Create(ctx.Context(), tenantID, req)
    if err != nil {
        return projectResponse{}, err
    }

    return toProjectResponse(project), nil
}
```

No business workflow and no query execution in handler.

## 9. Route Registration Pattern

Attach policies in required order:

1. auth
2. tenant
3. rbac
4. rate limit
5. cache
6. cache-control

Example route stack for GET /projects/{id}:

- AuthRequired
- TenantRequired
- RateLimit
- CacheRead

For write routes, add CacheInvalidate after auth/tenant/rbac/rate-limit policies.

## 10. DTO Suggestions

Request DTOs should:

- have clear json tags
- normalize/trim string values in validation or service layer
- return typed app errors for invalid input

Response DTOs should:

- map from domain model
- avoid leaking backend-specific fields unless intended API contract

## 11. Error Mapping Suggestions

Common mapping strategy:

- missing entity -> not_found
- unique conflict -> conflict
- invalid input -> bad_request
- dependency unavailable -> dependency_unavailable

Use typed app errors for predictable API behavior.

## 12. Minimal Test Matrix

Recommended tests:

- handler tests
  - request validation
  - success response shape
  - known error mapping
- service tests
  - write path uses transaction flow
  - read path uses direct repository flow
- repository tests
  - scan/mapping correctness
  - not-found and conflict behavior

## 13. Final CRUD Checklist

1. Domain model is backend-agnostic.
2. Repository interface is domain-focused.
3. Service write methods use transaction flow.
4. Service read methods avoid forced transaction wrappers.
5. Repository owns query/filter and mapping logic.
6. Store layer remains execution-only.
7. Route policy order is valid.
8. Tests/build/verify commands pass.

## 14. Related Docs

- [docs/modules.md](modules.md)
- [docs/module_guide.md](module_guide.md)
- [docs/policies.md](policies.md)
- [docs/cache-guide.md](cache-guide.md)
