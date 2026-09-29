# CRUD Examples

This guide walks through a realistic CRUD module using the enforced architecture:

Service -> Repository -> sqlc queries -> pgx (pool or transaction)

The example module name is projects.

## 1. What You Are Building

Routes:

- POST /api/v1/projects
- GET /api/v1/projects/{id}
- GET /api/v1/projects
- PUT /api/v1/projects/{id}
- DELETE /api/v1/projects/{id}

Each route follows:

handler -> service -> repository -> `DB().Queries(ctx)` / `DB().WithTx(ctx, fn)`

## 2. Data Model

Domain model (repo/service-facing):

```go
type Project struct {
    ID      string
    OwnerID string // the authenticated user who owns the project
    Name    string
    Status  string
}
```

Keep this model independent from database row structs and driver types.

## 3. Relational Setup (Postgres + sqlc)

The relational data layer is sqlc over pgx. To add tables and queries:

1. Add migration files under db/migrations (or keep module-local SQL under
   `internal/modules/projects/db/` and let modulesync copy it).
2. Mirror the schema shape under db/schema so sqlc can type-check.
3. Add query definitions under db/queries with sqlc annotations.
4. Run `make sqlc-generate` to regenerate typed Go into
   `internal/core/db/sqlcgen` (never edit that output by hand).

Example query source (`db/queries/projects.sql`):

```sql
-- name: CreateProject :one
INSERT INTO projects (id, owner_id, name, status)
VALUES ($1, $2, $3, $4)
RETURNING id, owner_id, name, status;

-- name: GetProjectByID :one
SELECT id, owner_id, name, status
FROM projects
WHERE owner_id = $1 AND id = $2;
```

Example migration (up):

```sql
CREATE TABLE IF NOT EXISTS projects (
    id         TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS projects_owner_id_idx ON projects (owner_id);
```

Note:

- generated sqlc types stay inside the repository
- do not leak sqlc/pgx types into service/repository public contracts

## 4. Repository Interface (Domain Contract)

Define repository interface in domain terms.

```go
type CreateProjectInput struct {
    ID      string
    OwnerID string
    Name    string
    Status  string
}

type ProjectRepository interface {
    Create(ctx context.Context, input CreateProjectInput) (Project, error)
    GetByID(ctx context.Context, ownerID, id string) (Project, error)
    List(ctx context.Context, ownerID string, limit int32) ([]Project, error)
    Update(ctx context.Context, ownerID, id string, name string, status string) (Project, error)
    Delete(ctx context.Context, ownerID, id string) error
}
```

Do not expose backend-specific types in this contract.

## 5. Service Layer (Workflow + Transactions)

The service calls repository methods and owns the transaction boundary for
writes via `DB().WithTx`. It holds the repository (which in turn holds the
`*storage.Postgres` boundary).

```go
type service struct {
    repo *Repo
}

func (s *service) Create(ctx context.Context, ownerID string, req createProjectRequest) (Project, error) {
    input := CreateProjectInput{
        ID:      newProjectID(),
        OwnerID: ownerID,
        Name:    strings.TrimSpace(req.Name),
        Status:  strings.TrimSpace(strings.ToLower(req.Status)),
    }

    var out Project
    err := s.repo.pg.WithTx(ctx, func(txCtx context.Context) error {
        created, err := s.repo.Create(txCtx, input) // runs inside the tx
        if err != nil {
            return err
        }
        out = created
        return nil
    })
    return out, err
}

func (s *service) GetByID(ctx context.Context, ownerID, id string) (Project, error) {
    return s.repo.GetByID(ctx, ownerID, id) // direct read, no tx
}
```

Rules shown above:

- write path uses `WithTx`; the repo call inside receives `txCtx`
- read path is a direct repository call
- the service constructs no SQL and runs no queries itself

## 6. Repository Implementation (sqlc)

The repository holds `*storage.Postgres` and, per method, obtains generated
queries via `Queries(ctx)` — which binds to the service's transaction when one
is active, or the pool otherwise. It maps generated rows to domain models.

```go
type Repo struct {
    pg *storage.Postgres
}

func NewRepo(pg *storage.Postgres) *Repo { return &Repo{pg: pg} }

func (r *Repo) Create(ctx context.Context, in CreateProjectInput) (Project, error) {
    row, err := r.pg.Queries(ctx).CreateProject(ctx, sqlcgen.CreateProjectParams{
        ID:      in.ID,
        OwnerID: in.OwnerID,
        Name:    in.Name,
        Status:  in.Status,
    })
    if err != nil {
        return Project{}, err
    }
    return mapProjectRow(row), nil
}

func (r *Repo) GetByID(ctx context.Context, ownerID, id string) (Project, error) {
    row, err := r.pg.Queries(ctx).GetProjectByID(ctx, sqlcgen.GetProjectByIDParams{
        OwnerID: ownerID,
        ID:      id,
    })
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return Project{}, ErrProjectNotFound
        }
        return Project{}, err
    }
    return mapProjectRow(row), nil
}

// mapProjectRow converts a generated row into the domain model, keeping
// sqlc/pgx types out of the service and public repository API.
func mapProjectRow(row sqlcgen.Project) Project {
    return Project{ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, Status: row.Status}
}
```

The same pattern used for a real feature is in
`internal/core/auth/user_repository.go`.

## 7. Document Repository Variant

If the module is document-backed instead, it holds a `document.Store` and calls
the collection API rather than `Queries(ctx)`. The repository contract stays the
same; only the implementation differs. See the worked example in
[docs/document-store.md](document-store.md).

Do not branch SQL/document behavior in one module implementation — pick one
backend per module.

## 8. Handler Layer Example

Handler reads transport input and delegates to service.

```go
func (h *Handler) Create(ctx *httpx.Context, req createProjectRequest) (projectResponse, error) {
    principal, ok := ctx.Auth()
    if !ok {
        return projectResponse{}, apperr.New(apperr.CodeUnauthorized, 401, "authentication required")
    }

    project, err := h.svc.Create(ctx.Context(), principal.UserID, req)
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
2. isolation (only when an optional feature adds a scope check; this example has none)
3. rbac
4. rate limit
5. cache
6. cache-control

Example route stack for GET /projects/{id}:

- AuthRequired
- RateLimit
- CacheRead (varies by `UserID`, because every project is owner-scoped)

For write routes, add CacheInvalidate after the auth/rbac/rate-limit policies.

An optional feature that scopes data (an organization, for example) adds its
own isolation policy and cache key part; see [policies.md](policies.md) and
[cache-guide.md](cache-guide.md#key-parts).

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
2. Repository interface is domain-focused (no sqlc/pgx types).
3. Service write methods use `WithTx`.
4. Service read methods avoid forced transaction wrappers.
5. Repository owns query/mapping logic via `Queries(ctx)`.
6. Repository threads `ctx` and never opens transactions.
7. Route policy order is valid.
8. Tests/build/verify commands pass.

## 14. Related Docs

- [docs/modules.md](modules.md)
- [docs/module_guide.md](module_guide.md)
- [docs/transactions.md](transactions.md)
- [docs/policies.md](policies.md)
- [docs/cache-guide.md](cache-guide.md)
