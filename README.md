[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/badge/release-v0.5.0-brightgreen)](CHANGELOG.md)

# SuperAPI

Production-grade Go API template for SaaS backends.

## IMPORTANT NOTICE

This is a TEMPLATE repository.

- Do NOT install via go get
- Use "Use this template" to create a new project
- Generated projects are independent and do NOT auto-update

## What This Is

SuperAPI is a modular Go API foundation focused on production use from day one.

It provides:
- a module-oriented API architecture
- policy-based middleware wiring
- built-in auth, caching, rate limiting, and observability primitives

Start here:
- Overview: [docs/overview.md](docs/overview.md)
- Architecture: [docs/architecture.md](docs/architecture.md)

## Features

- Module system for explicit, composable API domains
- Strict startup validation for runtime and policy configuration
- goAuth integration for route-level authentication workflows
- Redis-backed response cache and Redis-backed rate limiting
- Observability stack: metrics, tracing, and structured logs
- sqlc + pgx workflow for typed query access
- Built-in scaffolder for generating production-oriented modules

## Quick Start

```bash
1. Click "Use this template"
2. Clone your new repo
3. go run ./cmd/api
```

After startup:
- Liveness: `GET /healthz`
- Readiness: `GET /readyz`

### Minimal mode (no external dependencies)

Use the profile that disables Postgres, Redis, auth, cache, and rate limiting:

```bash
cp .env.example .env
# edit .env and enable:
# APP_PROFILE=minimal

go run ./cmd/api
```

### Full mode (Postgres + Redis + auth)

Use `.env.example` full-mode defaults, then run:

```bash
go run ./cmd/api
```

Required full-mode toggles are already shown in `.env.example`:
- `POSTGRES_ENABLED=true` + valid `POSTGRES_URL`
- `REDIS_ENABLED=true` + valid `REDIS_ADDR`
- `AUTH_ENABLED=true`
- `RATELIMIT_ENABLED=true`
- `CACHE_ENABLED=true`

## How To Build APIs

1. Create a module

```bash
make module name=projects
```

Expected output:

```text
generated module "projects" (package="projects" route=/api/v1/projects)
```

This creates:

- `internal/modules/projects/module.go`
- `internal/modules/projects/routes.go`
- `internal/modules/projects/dto.go`
- `internal/modules/projects/handler.go`
- `internal/modules/projects/service.go`
- `internal/modules/projects/repo.go`
- `internal/modules/projects/handler_test.go`
- `internal/modules/projects/service_test.go`

2. Confirm module wiring

`internal/modules/modules.go` is updated automatically with import + `projects.New()` entry.

3. Verify and run

```bash
go test ./internal/devx/modulegen ./internal/modules/projects
go run ./cmd/superapi-verify ./internal/modules/projects
go run ./cmd/api
```

4. Add handlers and service logic in the generated module files under `internal/modules/projects/`.

5. Add routes in `routes.go` and attach policies as needed (auth, tenant, rate limit, cache).

Guides:
- Module guide: [docs/modules.md](docs/modules.md)
- CRUD walkthrough: [docs/crud-examples.md](docs/crud-examples.md)

## Docs Navigation

- Overview: [docs/overview.md](docs/overview.md)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Modules: [docs/modules.md](docs/modules.md)
- Policies: [docs/policies.md](docs/policies.md)
- Cache guide: [docs/cache-guide.md](docs/cache-guide.md)
- Auth integration: [docs/auth-goauth.md](docs/auth-goauth.md)
- Auth bootstrap: [docs/auth-bootstrap.md](docs/auth-bootstrap.md)
- Performance runbook: [docs/performance-testing.md](docs/performance-testing.md)
- Environment variables: [docs/environment-variables.md](docs/environment-variables.md)
- Workflows: [docs/workflows.md](docs/workflows.md)

## Philosophy

- Secure by default in production-sensitive paths
- Explicit policies over implicit behavior
- Fail-fast validation at startup for unsafe configurations
- No hidden magic or global side effects where avoidable

## Versioning And Updates

- This template is distributed as a snapshot.
- Generated repositories do not receive automatic upstream updates.
- Upgrades are manual: compare changes, port intentionally, and validate with tests/build.
- Current public template baseline: `v0.5.0` (pre-1.0 by intent).

## Release Hygiene

Before publishing a downstream release:

```bash
go test ./...
go build ./...
```

## Contributing

Contribution process and governance rules are documented in [CONTRIBUTING.md](CONTRIBUTING.md).
