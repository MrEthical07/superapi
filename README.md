[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
<!-- template:begin maintainer -->
[![Release](https://img.shields.io/badge/release-v0.9.0-brightgreen)](CHANGELOG.md)
<!-- template:end maintainer -->

# SuperAPI

<!-- template:description -->
<!-- template:begin maintainer -->
Production-grade Go API template for SaaS backends.

## IMPORTANT NOTICE

This is a TEMPLATE repository.

- Do NOT install via go get
- Use "Use this template" to create a new project, then run `make init`
- Generated projects are independent and do NOT auto-update
<!-- template:end maintainer -->

## Quick Start

<!-- template:begin init -->
```bash
# 1. "Use this template" on GitHub, then clone your new repository
git clone https://github.com/acme/foo && cd foo

# 2. Make it yours: module path, README/CHANGELOG/LICENSE, optional pruning
make init module=github.com/acme/foo name="Foo API"
```
<!-- template:end init -->

```bash
make dev-up                                   # Postgres + Redis (docker compose)
cp .env.example .env
make migrate-up
make user email=admin@example.com role=admin  # prompts for the password
make run
```

Then `GET /healthz`, log in at `POST /api/v1/auth/login`, and call
`GET /api/v1/auth/whoami` with the token. Step-by-step guide:
[docs/getting-started.md](docs/getting-started.md).

No Docker? Before creating `.env`, `APP_PROFILE=minimal make run` starts the
API with Postgres, Redis, auth, cache and rate limiting disabled (values in
`.env` override the profile).

## What You Get

- **Module architecture** with an enforced data flow: handler -> service ->
  repository -> sqlc -> pgx, checked by a static verifier (`superapi-verify`).
- **Policy-ordered routes**: auth, tenant, RBAC, rate limit, cache and
  cache-control are declared per route and validated statically.
- **A complete auth lifecycle** on [goAuth](https://github.com/MrEthical07/goAuth)
  v0.5.0: login with remember-me, refresh, logout, logout-everywhere, sessions,
  password change, and opt-in registration, password reset, email
  verification, TOTP with backup codes, and WebAuthn. Enumeration-safe
  responses; secrets are delivered out-of-band, never in HTTP responses. See
  [docs/auth-flows.md](docs/auth-flows.md).
- **Multi-tenancy that is safe to turn on**: tenant resolution (header or
  subdomain), validation, tenant-scoped user lookup and token binding behind
  `TENANCY_ENABLED`. Off by default. See [docs/multi-tenancy.md](docs/multi-tenancy.md).
- **Redis-backed response cache and rate limiting** with explicit `VaryBy` and
  tag invalidation.
- **Observability**: Prometheus metrics, OpenTelemetry tracing, structured logs.
- **Fail-fast configuration**: unsafe or contradictory env combinations stop
  startup; every variable is listed in `.env.example`.
- **Clone-ready tooling**: `make init`, `make dev-up`, `make user`,
  `make doctor`, a distroless `Dockerfile`, and CI with sqlc drift and
  Postgres integration tests.
- **Genuinely optional features**: disable by config, or prune at init /
  delete later. See [docs/trim-to-what-you-need.md](docs/trim-to-what-you-need.md).

<!-- template:begin maintainer -->
## Why SuperAPI / Problems It Solves

| The problem you'd otherwise solve yourself | How SuperAPI solves it |
|---|---|
| **Auth lifecycle is more than login.** Registration, reset, verification, MFA, sessions, key rotation, abuse limiting — hand-rolling these is where security bugs live. | goAuth v0.5.0 wired end to end: every lifecycle endpoint, feature-flagged, enumeration-safe, with TOTP secrets encrypted at rest. |
| **Cache and rate-limit keys are a footgun.** | Policy-driven caching and rate limiting with explicit `VaryBy`/scope keying and tag-based invalidation. |
| **Multi-tenancy is hard to add later and risky to get wrong.** | One `TENANCY_ENABLED` flag: validated tenant resolution, tenant-scoped goAuth lookups (the v0.5.0 cross-tenant fixes), token-to-tenant binding. |
| **Data-access discipline erodes.** | One enforced flow checked by `superapi-verify`. |
| **Misconfiguration ships silently.** | Fail-fast startup lint; CI fails when an env var is read but undocumented. |
| **Templates lock you in.** | `make init --no-*` prunes whole features; the rest disable by config. |
<!-- template:end maintainer -->

## Data Layer Architecture

Enforced flow (relational):

Service -> Repository -> sqlc queries -> pgx (pool or transaction)

- services call repositories for all data operations and own write transactions
  via `storage.Postgres.WithTx(...)`
- repositories obtain sqlc queries via `storage.Postgres.Queries(ctx)` and own
  all query + mapping logic
- handlers never call the DB; sqlc/pgx types never appear on service/repository
  interfaces
<!-- template:begin document-store -->
- document persistence, when needed, comes from the optional
  `internal/storage/document` package wired per module
  ([docs/document-store.md](docs/document-store.md))
<!-- template:end document-store -->

## How To Build APIs

<!-- template:begin devx -->
1. Create a module: `make module name=projects` (add `db=1` for SQL scaffolding).
   `internal/modules/modules.go` is updated automatically.
<!-- template:end devx -->
1. Put the transport contract in `dto.go`, HTTP handling in `handler.go`,
   business logic in `service.go`, and queries + row mapping in `repo.go`.
1. Add SQL under `db/migrations`, `db/schema` and `db/queries`, then
   `make sqlc-generate`.
1. Register routes with explicit policies in `routes.go`.
1. Gate: `go build ./... && go test ./... && go run ./cmd/superapi-verify ./...`

Guides: [docs/modules.md](docs/modules.md),
[docs/crud-examples.md](docs/crud-examples.md), [AGENTS.md](AGENTS.md).

## Docs

- Getting started: [docs/getting-started.md](docs/getting-started.md)
- Overview: [docs/overview.md](docs/overview.md)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Modules: [docs/modules.md](docs/modules.md), [docs/module_guide.md](docs/module_guide.md)
- Transactions: [docs/transactions.md](docs/transactions.md)
- Policies: [docs/policies.md](docs/policies.md)
- Cache guide: [docs/cache-guide.md](docs/cache-guide.md)
- Auth: [docs/auth-flows.md](docs/auth-flows.md), [docs/auth-goauth.md](docs/auth-goauth.md), [docs/auth-bootstrap.md](docs/auth-bootstrap.md)
<!-- template:begin tenancy -->
- Multi-tenancy: [docs/multi-tenancy.md](docs/multi-tenancy.md), [docs/removing-tenancy.md](docs/removing-tenancy.md)
<!-- template:end tenancy -->
<!-- template:begin webauthn -->
- WebAuthn: [docs/enabling-webauthn.md](docs/enabling-webauthn.md)
<!-- template:end webauthn -->
<!-- template:begin document-store -->
- Document store (optional NoSQL): [docs/document-store.md](docs/document-store.md)
<!-- template:end document-store -->
- Trim to what you need: [docs/trim-to-what-you-need.md](docs/trim-to-what-you-need.md)
- Environment variables: [docs/environment-variables.md](docs/environment-variables.md)
- Security settings: [docs/security-env-recommendations.md](docs/security-env-recommendations.md)
<!-- template:begin perf -->
- Performance runbook: [docs/performance-testing.md](docs/performance-testing.md)
<!-- template:end perf -->
- Workflows: [docs/workflows.md](docs/workflows.md)
- Contributor playbook: [AGENTS.md](AGENTS.md)

## Philosophy

- Secure by default in production-sensitive paths
- Explicit policies over implicit behavior
- Fail-fast validation at startup for unsafe configurations
- One enforced data-layer architecture over compatibility layers
- Optional features are genuinely optional — disable by config, delete cleanly

## Acknowledgments

Authentication is powered by [goAuth](https://github.com/MrEthical07/goAuth),
an open-source authentication engine.

<!-- template:begin maintainer -->
## Showcase

- **ProjectBook**: A design thinking-first workspace for building people-centric projects without context fragmentation.
  - Frontend repository: [https://github.com/MrEthical07/projectbook](https://github.com/MrEthical07/projectbook)
  - Backend repository (built on SuperAPI): [https://github.com/MrEthical07/projectbook-backend](https://github.com/MrEthical07/projectbook-backend)

## Versioning And Updates

- This template is distributed as a snapshot; generated repositories do not
  receive automatic upstream updates.
- Upgrades are manual: compare changes, port intentionally, and validate with
  tests and the verifier. The CHANGELOG lists behavior changes per release.
- Current public template baseline: v0.9.0 (pre-1.0 by intent).

## Release Hygiene

Template releases follow the checklist in [CONTRIBUTING.md](CONTRIBUTING.md).
<!-- template:end maintainer -->

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
