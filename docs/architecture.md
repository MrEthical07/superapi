# Architecture

This document explains how SuperAPI works internally, from process startup to request handling to data access.

It is written for both:

- beginners who want a mental model of the system
- contributors who need precise behavior before changing core code

## 1. Architecture Principles

The enforced data flow is:

Service -> Repository -> sqlc queries -> pgx (pool or transaction)

Layer boundaries are strict:

- Handler layer:
	- transport concerns only
	- no business or data-access logic
- Service layer:
	- business workflows and orchestration
	- calls repositories only
	- may call `storage.Postgres.WithTx(...)` to define a write transaction
	  boundary, but never runs queries itself
- Repository layer:
	- query logic and row/domain mapping
	- obtains generated sqlc queries via `storage.Postgres.Queries(ctx)`
	- does not control transaction boundaries
- Data-access boundary (`storage.Postgres`):
	- hands repositories sqlc queries bound to the request transaction (when one
	  is active) or the pool
	- owns the transaction lifecycle via `WithTx`
	- exposes no query surface of its own; sqlc/pgx types never appear on
	  service or repository interfaces

These boundaries are not style suggestions. They are the architecture contract
for this repository, and the `superapi-verify` static checker fails the build on
violations.

## 2. Repository Layout

High-impact paths:

- cmd/api/main.go
	- process entrypoint
- internal/core/app
	- runtime app container and dependency wiring
- internal/core/httpx
	- router integration and global middleware assembly
- internal/core/policy
	- route policy chain, metadata, and route validation
- internal/core/storage
	- the `storage.Postgres` data-access boundary (sqlc queries + `WithTx`)
- internal/core/db/sqlcgen
	- sqlc-generated query code (do not edit by hand)
- internal/storage/document
	- optional, self-contained document (NoSQL) store (not in the binary until a
	  module imports it)
- internal/core/auth
	- goAuth integration, sqlc-backed user provider, auth repository
- internal/modules
	- feature modules and route registration

## 3. Startup Sequence

Startup begins in cmd/api/main.go.

Process flow:

1. Load config from environment.
2. Lint config and fail fast on invalid combinations.
3. Initialize logger.
4. Build app via app.New(...).
5. Register modules from internal/modules/modules.go.
6. Start server and wait for shutdown signal.

### 3.1 app.New responsibilities

app.New performs:

- router initialization
- dependency initialization via initDependencies
- optional metrics route registration
- global middleware assembly
- module dependency binding
- module route registration

If any module registration fails, initialized dependencies are closed and startup aborts.

### 3.2 Dependency initialization order

Dependency wiring is in internal/core/app/deps.go.

Order:

1. Create readiness service.
2. If Postgres enabled:
	 - create pgx pool
	 - create the `storage.Postgres` boundary from the pool
	 - set Dependencies.DB
	 - register readiness probe
3. If Redis enabled:
	 - create redis client
	 - register readiness probe
4. Create metrics service.
5. Parse auth mode; apply the tenancy flag (`TENANCY_ENABLED`).
6. If auth enabled:
	 - create the auth user repository over the `storage.Postgres` boundary
	 - create the sqlc-backed `StoreUserProvider` (optionally with the WebAuthn
	   credential repository)
	 - create the goAuth engine (v0.4.0) with Redis + provider + tenancy settings
7. If rate-limit enabled:
	 - create redis limiter
8. If cache enabled:
	 - create cache manager
9. Create tracing service.

Failure model:

- Any enabled critical dependency failing startup aborts the process.
- Resources initialized earlier are closed before returning startup error.

## 4. Request Lifecycle

### 4.1 Global middleware pipeline

Global middleware assembly is in internal/core/httpx/globalmiddleware.go.

Execution order (outermost to innermost):

1. RequestID
2. ClientIP
3. Recoverer
4. CORS
5. SecurityHeaders
6. MaxBodyBytes
7. RequestTimeout
8. Tracing
9. AccessLog
10. Router dispatch

Why this order matters:

- request id is available to downstream logs/errors
- panic recovery wraps route execution safely
- timeout/tracing/logging capture actual route execution behavior

### 4.2 Route policy chain

Route policies are composed using policy.Chain in internal/core/policy/policy.go.

If route registers policies [P1, P2, P3], execution is:

- request: P1 -> P2 -> P3 -> handler
- response unwind: handler -> P3 -> P2 -> P1

### 4.3 Route validation

Before route behavior is finalized, policy validation enforces safe stacks.

Validation code is in:

- internal/core/policy/validator.go
- internal/core/policy/validator_rules.go

Key validations:

- policy stage ordering
- auth prerequisites for RBAC/tenant policies
- tenant path rules for routes with tenant_id path params
- cache safety rules on authenticated routes (must vary by user or tenant)

## 5. Handler, Adapter, and Response Model

Handlers are typed and adapted through internal/core/httpx/adapter.go.

Adapter behavior:

- decode JSON (for body-carrying request types)
- run request validation
- execute typed handler function
- map errors through response.Error
- wrap output in standard envelope

Standard response envelope is defined in internal/core/response/response.go.

Success shape:

- ok: true
- data: payload
- request_id

Error shape:

- ok: false
- error.code
- error.message
- optional error.details
- request_id

### 5.1 Error mapping summary

response.Error maps:

- context deadline exceeded -> timeout response
- typed AppError -> explicit status/code/message
- unknown errors -> internal_error (sanitized)

## 6. The Data-Access Boundary

The relational data layer is a single thin type, `storage.Postgres`, defined in
internal/core/storage/postgres_store.go. It deliberately exposes no query surface
of its own — just two methods:

- `Queries(ctx) *sqlcgen.Queries`
	- returns generated sqlc queries bound to the transaction carried in `ctx`
	  (when one is active) or to the pool otherwise
	- repositories call this per operation; the same code runs on the pool for
	  reads and inside a transaction for writes, transparently
- `WithTx(ctx, fn) error`
	- begins a pgx transaction, stashes it in the context passed to `fn`, and
	  commits on success or rolls back on error/panic
	- services call this to define write boundaries; repositories never do

Design goal: sqlc/pgx types are implementation details. They live inside
repositories and never appear on service or repository public interfaces.

### 6.1 How binding works

`WithTx` stores the `pgx.Tx` in the context under a private key. `Queries(ctx)`
checks for it: if present it binds the generated queries to the transaction,
otherwise to the pool. This is why a repository method written once works both
standalone and inside a service-owned transaction — it simply threads `ctx`
through and calls `r.pg.Queries(ctx).SomeGeneratedMethod(ctx, ...)`.

### 6.2 Generated code

sqlc output lives in internal/core/db/sqlcgen and is regenerated by
`make sqlc-generate`. It must not be edited by hand. SQL sources are under
`db/schema` and `db/queries`; module-local SQL is synced into that tree by
modulesync before generation.

### 6.3 Optional document (NoSQL) store

A separate, self-contained package, internal/storage/document, provides an
optional document store for modules that need one. It is outside `internal/core`
and is excluded from the `cmd/api` binary until a module imports it. It shares
nothing with the relational boundary or the Redis response cache. See
[docs/document-store.md](document-store.md).

## 7. Transaction Model

Transaction rule set:

- the transaction boundary lives on `storage.Postgres.WithTx`
- write paths run inside `DB().WithTx(ctx, fn)`
- read paths are direct by default (no transaction)
- services select the transaction boundary; repositories only run queries via
  `Queries(ctx)` and never begin/commit/rollback

Write path example:

1. handler calls service.Create
2. service calls `DB().WithTx(ctx, func(txCtx) error { ... })`
3. inside the callback, service calls repository write methods with `txCtx`
4. repository runs `pg.Queries(txCtx).<GeneratedWrite>(...)`, which joins the tx
5. `WithTx` commits on nil error, rolls back on error or panic

Read path example:

1. handler calls service.Get/List
2. service calls the repository directly (no `WithTx`)
3. repository runs `pg.Queries(ctx).<GeneratedRead>(...)` on the pool

The one sharp edge: the transaction lives entirely in the context. If a
repository method drops `txCtx` and uses a fresh context, its query silently
runs on the pool outside the transaction — no error, just a correctness bug.
Always thread the context through. See [docs/transactions.md](transactions.md).

## 8. Auth Architecture With goAuth

SuperAPI is on goAuth **v0.4.0**. The engine is built in
internal/core/auth/goauth_provider.go and receives a `goauth.UserProvider`.

Current provider implementation: internal/core/auth/provider_store.go
(`StoreUserProvider`), which also implements goAuth's
`WebAuthnCredentialProvider` when WebAuthn is enabled.

Provider path (sqlc data layer):

StoreUserProvider -> UserRepository -> storage.Postgres (sqlc queries) -> pgx

Auth repository implementation: internal/core/auth/user_repository.go — it uses
`pg.Queries(ctx)` like any other repository and maps generated rows to a
storage-layer `StoredUser` projection. v0.4.0 configuration (remember-me,
session ceiling, MFA, sliding-window limiter, key rotation, WebAuthn) is set in
internal/core/auth/config.go. See [docs/auth-goauth.md](auth-goauth.md).

## 9. Route-Level Flow Examples

### 9.1 POST /api/v1/system/auth/login

Files involved:

- internal/modules/system/routes.go
- internal/modules/system/service.go (thin authService)
- internal/core/auth/provider_store.go
- internal/core/auth/user_repository.go
- internal/core/storage/postgres_store.go

Runtime path:

1. route handler receives login payload and calls the module's authService
2. authService calls the goAuth engine (`LoginWithOptions`, honoring remember-me)
3. goAuth asks StoreUserProvider for the user by identifier
4. provider calls the auth repository
5. repository runs `pg.Queries(ctx).GetAuthUserByLogin(...)` on the pool
6. the generated row maps back to a goAuth user record
7. goAuth issues tokens, or returns an MFA challenge if a second factor is required

### 9.2 POST /api/v1/system/auth/refresh

High-level path:

- handler calls goAuth refresh
- goAuth performs token/session validation
- provider/repository/store path is used when user persistence reads are required

### 9.3 GET /api/v1/system/whoami

Path:

1. AuthRequired validates request and injects auth context
2. handler reads auth context and returns payload
3. no repository/store call required for this endpoint

## 10. Readiness, Health, And Shutdown

### 10.1 Liveness vs readiness

- /healthz:
	- process-level liveness
- /readyz:
	- dependency readiness from readiness service checks

### 10.2 Shutdown sequence

During app shutdown:

1. server shutdown with configured timeout
2. close redis
3. close postgres
4. shutdown tracing
5. close auth engine resources

## 11. Data Layer At A Glance

What the data layer guarantees:

- one enforced path — Service -> Repository -> sqlc -> pgx — with no second
  pattern to drift toward
- a single thin transaction boundary (`storage.Postgres.WithTx`) owned by
  services; repositories never manage transactions
- sqlc/pgx types stay inside repositories and never leak onto public interfaces
- auth persistence follows the same repository pattern as any module
- the optional document store is separate and out of the binary until used

What stays stable across changes:

- module registration model
- route policy system
- goAuth integration boundary (a `goauth.UserProvider`)
- response envelope semantics

## 12. Contributor Guardrails

When changing architecture-sensitive code, keep these guardrails:

- do not bypass policy validation
- do not move business logic into handlers
- do not expose backend-specific driver/query objects in service/repository interfaces
- do not mix relational and document backends in one module
- do not manually edit generated files under internal/core/db/sqlcgen

## 13. Related Docs

- [docs/overview.md](overview.md)
- [docs/modules.md](modules.md)
- [docs/module_guide.md](module_guide.md)
- [docs/crud-examples.md](crud-examples.md)
- [docs/transactions.md](transactions.md)
- [docs/auth-goauth.md](auth-goauth.md)
- [docs/document-store.md](document-store.md)
- [docs/workflows.md](workflows.md)
- [docs/environment-variables.md](environment-variables.md)
