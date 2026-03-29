# Day-to-Day Workflows

---

For the full runtime environment variable matrix (defaults, behavior, and constraints), see [docs/environment-variables.md](environment-variables.md).

## 1. Running the server

### Minimal (no external dependencies)

```bash
go run ./cmd/api
```

Server starts on `:8080`. Postgres and Redis features are disabled. Health, system, and tenant endpoints (non-DB ones) are available.

### With all features

```bash
export POSTGRES_ENABLED=true
export POSTGRES_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
export REDIS_ENABLED=true
export REDIS_ADDR="127.0.0.1:6379"
export AUTH_ENABLED=true
export AUTH_MODE=hybrid
export RATELIMIT_ENABLED=true
export CACHE_ENABLED=true

go run ./cmd/api
```

### Changing listen address

```bash
HTTP_ADDR=":3000" go run ./cmd/api
```

### Dev-friendly logging

```bash
LOG_FORMAT=text LOG_LEVEL=debug go run ./cmd/api
```

---

## 2. Creating a new module

### Using the scaffolder (recommended)

```bash
make module name=projects
```

This creates `internal/modules/projects/` with:
- `module.go` — Module struct, `Name()`, `app.Module` interface
- `routes.go` — `Register()` with a default ping endpoint and policy examples
- `dto.go` — Request/response types with `Validate()` example
- `handler.go` — Handler struct with `Ping()` method
- `service.go` — Service struct with `Ping()` method
- `repo.go` — Empty repo struct
- `handler_test.go` — Ping handler test
- `service_test.go` — Ping service test

It also updates `internal/modules/modules.go`:
- Adds the import: `"github.com/MrEthical07/superapi/internal/modules/projects"`
- Adds to `All()`: `projects.New()`

### Overwriting an existing module

```bash
make module name=projects force=1
```

### Advanced scaffolding flags

```bash
# Add module-local db/schema.sql + db/queries.sql stubs
make module name=projects db=1

# Add policy-ready route wiring examples
make module name=projects auth=1 tenant=1 ratelimit=1 cache=1

# Also create a global migration scaffold (requires db=1)
make module name=projects db=1 migration=1
```

Generator constraints:
- `tenant=1` requires `auth=1`
- `migration=1` requires `db=1`

### Name normalization rules

| Input | Package name | Route path |
|---|---|---|
| `projects` | `projects` | `/api/v1/projects` |
| `project_tasks` | `project_tasks` | `/api/v1/project-tasks` |
| `project-tasks` | `project_tasks` | `/api/v1/project-tasks` |

Requirements:
- Must be lowercase
- Only `a-z`, `0-9`, `-`, `_` allowed
- Must start with a letter

### After scaffolding

1. Verify: `go build ./...`
2. Run tests: `go test ./...`
3. Start server and test the ping endpoint: `curl http://localhost:8080/api/v1/projects/ping`
4. Begin adding real routes, handlers, services, and repos

### Bootstrapping auth schema/provider (optional)

```bash
# Interactive auth bootstrap wizard
make auth

# Config-driven auth bootstrap
make auth-config file=authgen.example.yaml
```

This generates/updates auth bootstrap outputs, including `docs/auth-bootstrap.md`.

---

## 3. Database workflow

### Creating a migration

```bash
# Using golang-migrate CLI
migrate create -ext sql -dir db/migrations -seq add_projects_table

# Using make
make migrate-create NAME=add_projects_table
```

This creates two empty files:
- `db/migrations/000003_add_projects_table.up.sql`
- `db/migrations/000003_add_projects_table.down.sql`

### Writing migration SQL

**Up migration** (`000003_add_projects_table.up.sql`):
```sql
CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_projects_tenant_id ON projects(tenant_id);
```

**Down migration** (`000003_add_projects_table.down.sql`):
```sql
DROP TABLE IF EXISTS projects;
```

### Updating schema mirror

After writing migrations, update `db/schema/` to reflect the final table shape. sqlc reads from `db/schema/`, not from migrations.

Create `db/schema/projects.sql`:
```sql
CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Adding SQL queries

Create `db/queries/projects.sql`:
```sql
-- name: CreateProject :one
INSERT INTO projects (id, tenant_id, name, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;

-- name: ListProjectsByTenant :many
SELECT * FROM projects WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;
```

### Regenerating sqlc

```bash
# With sqlc installed
sqlc generate

# Without sqlc installed
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate

# Using make
make sqlc-generate
```

This regenerates files in `internal/core/db/sqlcgen/`. **Never edit those files manually.**

### Running migrations

```bash
# Apply all pending migrations
POSTGRES_ENABLED=true POSTGRES_URL="$DB_URL" go run ./cmd/migrate up

# Roll back one step
POSTGRES_ENABLED=true POSTGRES_URL="$DB_URL" go run ./cmd/migrate down --steps=1

# Check current version
POSTGRES_ENABLED=true POSTGRES_URL="$DB_URL" go run ./cmd/migrate version

# Force a version (for fixing dirty state)
POSTGRES_ENABLED=true POSTGRES_URL="$DB_URL" go run ./cmd/migrate force --version=2

# Using make
make migrate-up DB_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
make migrate-down DB_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
make migrate-version DB_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
```

### Complete DB workflow checklist

1. `migrate create -ext sql -dir db/migrations -seq add_feature_table`
2. Write up/down SQL in the new migration files
3. Mirror final schema in `db/schema/<table>.sql`
4. Add queries in `db/queries/<table>.sql`
5. Run `sqlc generate`
6. Run `go build ./...` to verify generated code
7. Apply migration: `go run ./cmd/migrate up`
8. Run `go test ./...`

---

## 4. Enabling/disabling features via env

All features are controlled by environment variables. No code changes needed to toggle features.

### Auth

```bash
AUTH_ENABLED=true AUTH_MODE=hybrid   # Enable with hybrid mode
AUTH_ENABLED=false                    # Disable (default)
```

Requires: `REDIS_ENABLED=true` and `POSTGRES_ENABLED=true`

### Rate limiting

```bash
RATELIMIT_ENABLED=true               # Enable
RATELIMIT_FAIL_OPEN=true             # Allow requests when Redis is down (default)
RATELIMIT_DEFAULT_LIMIT=100           # Requests per window
RATELIMIT_DEFAULT_WINDOW=1m           # Window duration
```

Requires: `REDIS_ENABLED=true`

### Response caching

```bash
CACHE_ENABLED=true                    # Enable
CACHE_FAIL_OPEN=true                  # Bypass cache when Redis is down (default)
CACHE_DEFAULT_MAX_BYTES=262144        # Max cached response size (256 KiB)
```

Requires: `REDIS_ENABLED=true`

### Tracing

```bash
TRACING_ENABLED=true
TRACING_EXPORTER=otlpgrpc
TRACING_OTLP_ENDPOINT=localhost:4317
TRACING_SAMPLER=traceidratio
TRACING_SAMPLE_RATIO=0.05
TRACING_INSECURE=true
```

### Security headers

```bash
HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED=true
```

### Request timeout

```bash
HTTP_MIDDLEWARE_REQUEST_TIMEOUT=10s     # Must be <= HTTP_WRITE_TIMEOUT
```

### Body size limit

```bash
HTTP_MIDDLEWARE_MAX_BODY_BYTES=1048576  # 1 MiB global limit
```

### Access logging

```bash
HTTP_MIDDLEWARE_ACCESS_LOG_ENABLED=true
HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE=1.0   # Log all requests (dev)
HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD=500ms
```

---

## 5. Testing workflows

### Run all tests

```bash
go test ./...

# With race detector
go test ./... -race

# Using make
make test
```

### Run a specific package

```bash
go test ./internal/modules/system/...
```

### Run a specific test

```bash
go test ./internal/modules/system/ -run TestWhoamiRequiresAuth
```

### Build check (no tests)

```bash
go build ./...
```

### Format and vet

```bash
make fmt
make vet
```

### Full pre-commit check

```bash
make fmt
make vet
make test
go build ./...
```

---

## 6. Common gotchas

### "auth enabled requires redis enabled"

Config lint enforces that `AUTH_ENABLED=true` requires both `REDIS_ENABLED=true` and `POSTGRES_ENABLED=true`. Rate limiting and caching still require Redis only.

### "postgres url cannot be empty when enabled"

When `POSTGRES_ENABLED=true`, you must also set `POSTGRES_URL`.

### sqlc regeneration after schema changes

If you change `db/schema/` or `db/queries/` but forget to run `sqlc generate`, the generated code in `internal/core/db/sqlcgen/` will be stale and the build may fail.

### Migration "no change"

If `go run ./cmd/migrate up` reports no change, all migrations are already applied. This is treated as success.

### Module generator won't overwrite

By default, `make module name=existing` fails if the directory exists. Use `force=1` to overwrite.

### Nil pool / nil service checks

Modules must handle the case where dependencies are disabled. Check for nil pool/service and return `apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")`.
