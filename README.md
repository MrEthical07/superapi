[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/badge/release-v0.7.0-brightgreen)](CHANGELOG.md)

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
- an sqlc-based relational data layer with strict boundaries
- an optional, deletable document (NoSQL) store for modules that need it

Start here:
- Overview: [docs/overview.md](docs/overview.md)
- Architecture: [docs/architecture.md](docs/architecture.md)

## Why SuperAPI / Problems It Solves

Standing up a production Go SaaS backend from scratch means solving the same
hard, easy-to-get-wrong problems every time. Most templates hand you folders and
leave those problems to you. SuperAPI solves them, and enforces the solution so
it does not rot as the codebase grows.

| The problem you'd otherwise solve yourself | How SuperAPI solves it |
|---|---|
| **Auth lifecycle is more than login.** Refresh, graceful logout, remember-me, MFA, session ceilings, key rotation, abuse limiting — hand-rolling these is where security bugs live. | A real auth engine ([goAuth](https://github.com/MrEthical07/goAuth) v0.4.0) wired at the route level: remember-me, graceful logout, MFA-aware login, sliding-window abuse limiting, Ed25519 key rotation, and WebAuthn (scaffolded, off by default). Not a toy JWT snippet. |
| **Cache and rate-limit keys are a footgun.** Cache a response under the wrong key and you leak one user's data to another; key a limiter wrong and you throttle everyone or no one. | Policy-driven caching and rate limiting with explicit `VaryBy`/scope keying and tag-based invalidation — the keying is declared per route, not improvised in handlers. |
| **Multi-tenancy is hard to add later and risky to get wrong.** Isolation leaks are catastrophic and retrofitting tenant-awareness touches everything. | Tenancy lives behind one `TENANCY_ENABLED` flag with clean policy seams. Off by default with zero cost; when on, tenant isolation is a first-class policy — and it deletes cleanly if you never need it. |
| **Data-access discipline erodes.** One "quick" raw query in a handler, one service reaching past its repository, and the architecture is gone. | One enforced flow — Service → Repository → sqlc → pgx — checked by a static verifier (`superapi-verify`) that fails the build on violations. There is no second pattern to drift toward. |
| **Misconfiguration ships silently.** A bad env combo (enforce isolation without tenancy, auth without its backing store) becomes a 3 a.m. incident. | Fail-fast startup linting rejects unsafe/contradictory configuration before the server accepts traffic. |
| **Observability is always "later".** Metrics, tracing, and structured logs get bolted on after the incident that needed them. | Metrics, tracing, and structured logging are wired into the middleware chain from day one — on by default, toggleable, not an afterthought. |
| **Templates lock you in.** You inherit Mongo, or auth, or a cache you don't want, and ripping it out breaks everything. | Every optional subsystem is genuinely optional: disable by config (zero code) or delete cleanly (bounded, greppable). See [docs/trim-to-what-you-need.md](docs/trim-to-what-you-need.md). |

## Highlights

- **One enforced data-layer architecture, not two.** Service → Repository → sqlc → pgx, with a single thin transaction boundary. No if-sql-else-mongo branching, no parallel "raw query" escape hatch — the static verifier keeps it that way.
- **Policy-ordered middleware with fail-fast verification.** Auth, tenant, RBAC, rate-limit, cache, and cache-control are declared as ordered route policies and checked statically, so unsafe wiring fails the build instead of production.
- **Secure by default on production-sensitive paths.** Sensible defaults for auth, keying, and configuration; unsafe combinations are rejected at startup rather than tolerated.
- **A real auth engine.** goAuth v0.4.0 covers the full identity lifecycle (remember-me, MFA, graceful logout, key rotation, WebAuthn) instead of a login handler you have to grow yourself.
- **Genuinely optional features.** Tenancy, the document store, WebAuthn, caching, rate limiting, observability, and the DevX generators each cost nothing when off and delete cleanly when unwanted — plug-and-play works in both directions.
- **A document (NoSQL) store that stays out of the way.** Optional, self-contained, excluded from the binary until a module imports it, backend-swappable (a MongoDB adapter is documented), and it shares nothing with the Redis response cache.
- **Scaffolding that follows the rules.** `make module` and `make auth` generate code in the enforced architecture, so the fast path is also the correct path.

**Kept honest:** SuperAPI is a *template*, distributed as a snapshot — generated
projects are independent and do not auto-update. It is pre-1.0 by intent; expect
to port upgrades manually and validate with tests and the verifier.

## Data Layer Architecture

Enforced flow (relational):

Service -> Repository -> sqlc queries -> pgx (pool or transaction)

Hard rules:
- services call repositories for all data operations
- services may call storage.Postgres.WithTx(...) only to define transaction boundaries for write operations; they must not call sqlc/pgx directly
- repositories obtain sqlc queries via storage.Postgres.Queries(ctx) and own all query + mapping logic
- repositories must not control transaction boundaries
- handlers never call the DB directly
- sqlc/pgx types are implementation details and must not appear on service/repository interfaces
- document persistence, when needed, comes from the optional internal/storage/document package wired per module (no if-sql-else-mongo branching)

See [docs/document-store.md](docs/document-store.md) for the optional document store and a drop-in MongoDB adapter.

## Features

- Module system for explicit, composable API domains
- Strict startup validation for runtime and policy configuration
- goAuth v0.4.0 integration for route-level auth: remember-me, graceful logout, MFA-aware login, sliding-window abuse limiting, Ed25519 key rotation, and WebAuthn (scaffolded, off by default)
- Optional multi-tenancy behind a single `TENANCY_ENABLED` flag with clean seams (off by default; cleanly deletable)
- sqlc-based relational data layer with a thin transaction boundary and strict repository/service boundaries
- Optional, self-contained document (NoSQL) store — wire it per module or delete it; a MongoDB adapter is documented and it shares nothing with the response cache
- Redis-backed response cache with dynamic TagSpecs invalidation and Redis-backed rate limiting
- Browser/proxy cache directives with policy.CacheControl(...)
- Observability stack: metrics, tracing, and structured logs
- Built-in scaffolder for generating production-oriented modules

## Acknowledgments

SuperAPI uses **goAuth** as its authentication engine.

`goAuth` is an open-source authentication framework that powers SuperAPI's route-level auth workflows and identity lifecycle integration.

- goAuth repository: [https://github.com/MrEthical07/goAuth](https://github.com/MrEthical07/goAuth)

## Showcase

The following project uses SuperAPI as its backend foundation in production-oriented development:

- **ProjectBook**: A design thinking-first workspace for building people-centric projects without context fragmentation.
  - Frontend repository: [https://github.com/MrEthical07/projectbook](https://github.com/MrEthical07/projectbook)
  - Backend repository (built on SuperAPI): [https://github.com/MrEthical07/projectbook-backend](https://github.com/MrEthical07/projectbook-backend)

## Quick Start

```bash
1. Click "Use this template"
2. Clone your new repo
3. go run ./cmd/api
```

After startup:
- Liveness: GET /healthz
- Readiness: GET /readyz

### Minimal mode (no external dependencies)

Use the profile that disables Postgres, Redis, auth, cache, and rate limiting:

```bash
cp .env.example .env
# edit .env and enable:
# APP_PROFILE=minimal

go run ./cmd/api
```

### Full mode (Postgres + Redis + auth)

Use .env.example full-mode defaults, then run:

```bash
go run ./cmd/api
```

Required full-mode toggles are already shown in .env.example:
- POSTGRES_ENABLED=true with valid POSTGRES_URL
- REDIS_ENABLED=true with valid REDIS_ADDR
- AUTH_ENABLED=true
- RATELIMIT_ENABLED=true
- CACHE_ENABLED=true

## How To Build APIs

1. Create a module

```bash
make module name=projects
```

Expected output:

```text
generated module "projects" (package="projects" route=/api/v1/projects)
```

2. Confirm module wiring

internal/modules/modules.go is updated automatically with import + projects.New() entry.

3. Verify and run

```bash
go test ./internal/devx/modulegen ./internal/modules/projects
go run ./cmd/superapi-verify ./internal/modules/projects
go run ./cmd/api
```

4. Add handlers and service logic in the generated module files.

5. Add repositories that execute store operations and keep query logic inside repository.

Guides:
- Module guide: [docs/modules.md](docs/modules.md)
- CRUD walkthrough: [docs/crud-examples.md](docs/crud-examples.md)
- Contributor playbook: [AGENTS.md](AGENTS.md)

## Docs Navigation

- Overview: [docs/overview.md](docs/overview.md)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Modules: [docs/modules.md](docs/modules.md)
- Policies: [docs/policies.md](docs/policies.md)
- Cache guide: [docs/cache-guide.md](docs/cache-guide.md)
- Document store (optional NoSQL): [docs/document-store.md](docs/document-store.md)
- Trim to what you need (disable/delete features): [docs/trim-to-what-you-need.md](docs/trim-to-what-you-need.md)
- Auth integration: [docs/auth-goauth.md](docs/auth-goauth.md)
- Auth bootstrap: [docs/auth-bootstrap.md](docs/auth-bootstrap.md)
- Performance runbook: [docs/performance-testing.md](docs/performance-testing.md)
- Environment variables: [docs/environment-variables.md](docs/environment-variables.md)
- Workflows: [docs/workflows.md](docs/workflows.md)
- Contributor playbook: [AGENTS.md](AGENTS.md)

## Trim To What You Need

Plug-and-play works in both directions: every optional subsystem can be turned
off with a config flag (zero code) and most can be deleted cleanly. Auth,
tenancy, WebAuthn, the document store, caching, rate limiting, observability, and
the DevX generators are all optional — use only what you need and delete the
rest.

See **[docs/trim-to-what-you-need.md](docs/trim-to-what-you-need.md)** for the
per-feature "disable vs delete" checklist (exact files and wiring edits). Related:
[docs/removing-tenancy.md](docs/removing-tenancy.md),
[docs/enabling-webauthn.md](docs/enabling-webauthn.md).

## Philosophy

- Secure by default in production-sensitive paths
- Explicit policies over implicit behavior
- Fail-fast validation at startup for unsafe configurations
- One enforced data-layer architecture over compatibility layers
- Optional features are genuinely optional — disable by config, delete cleanly

## Versioning And Updates

- This template is distributed as a snapshot.
- Generated repositories do not receive automatic upstream updates.
- Upgrades are manual: compare changes, port intentionally, and validate with tests/build.
- Current public template baseline: v0.7.0 (pre-1.0 by intent).

## Release Hygiene

Before publishing a downstream release:

```bash
go test ./...
go build ./...
```

## Contributing

Contribution process and governance rules are documented in [CONTRIBUTING.md](CONTRIBUTING.md).
