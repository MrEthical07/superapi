# Architecture

This document explains how SuperAPI works internally, from process startup to request handling to data access.

It is written for both:

- beginners who want a mental model of the system
- contributors who need precise behavior before changing core code

## 1. Architecture Principles

The enforced data flow is:

Service -> Repository -> Store -> Backend

Layer boundaries are strict:

- Handler layer:
	- transport concerns only
	- no business or data-access logic
- Service layer:
	- business workflows and orchestration
	- calls repositories only
- Repository layer:
	- query logic and storage mapping
	- calls store interfaces only
- Store layer:
	- execution and transaction semantics
	- no domain-level behavior

These boundaries are not style suggestions. They are the architecture contract for this repository.

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
	- store contracts and store implementations
- internal/core/auth
	- goAuth integration, store-backed user provider, auth repository
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
	 - create PostgresRelationalStore
	 - set Dependencies.Postgres, Dependencies.RelationalStore, Dependencies.Store
	 - register readiness probe
3. If Redis enabled:
	 - create redis client
	 - register readiness probe
4. Create metrics service.
5. Parse auth mode.
6. If auth enabled:
	 - create auth user repository over relational store
	 - create store-backed user provider
	 - create goAuth engine
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

## 6. Store Contracts And Data Layer

Store contracts are in internal/core/storage/contracts.go.

Core contracts:

- Store
	- Kind() for backend family identity
- TransactionalStore
	- WithTx(ctx, fn)
- RelationalStore
	- Execute(ctx, RelationalOperation)
- DocumentStore
	- Execute(ctx, DocumentOperation)

Important design goal:

- stores are execution-only contracts
- repository owns query meaning and mapping semantics

### 6.1 Relational store implementation

Current relational implementation is in internal/core/storage/postgres_store.go.

Key behavior:

- Execute chooses transaction runner from context when present
- otherwise uses pool runner
- WithTx starts pgx transaction, injects tx runner in context, commits or rolls back

### 6.2 Document store status

internal/core/storage/document_noop.go provides NoopDocumentStore.

Purpose:

- preserve the architecture contract for document modules now
- allow compilation and interface wiring before concrete document backend arrives

### 6.3 Repository-owned operations

Operation helpers in internal/core/storage/operations.go provide wrappers like:

- RelationalExec
- RelationalQueryOne
- RelationalQueryMany
- DocumentRun

Repositories use these helpers to describe operations while keeping domain logic in repository methods.

## 7. Transaction Model

Transaction rule set:

- transaction API is mandatory at store layer
- write paths should run inside store.WithTx
- read paths are direct by default
- services select transaction boundary; repositories execute operations

Write path example:

1. handler calls service.Create
2. service starts store.WithTx
3. repository executes write operations via store.Execute
4. store commits or rolls back

Read path example:

1. handler calls service.Get/List
2. service calls repository directly
3. repository executes read operation via store.Execute

## 8. Auth Architecture With goAuth

Auth integration entrypoint: internal/core/auth/goauth_provider.go.

goAuth boundary remains stable: it still receives a goauth.UserProvider.

Current provider implementation: internal/core/auth/provider_sqlc.go.

Provider path:

StoreUserProvider -> UserRepository -> RelationalStore -> Postgres

Auth repository implementation: internal/core/auth/user_repository.go.

This preserves goAuth compatibility while removing direct query-object coupling from auth wiring.

## 9. Route-Level Flow Examples

### 9.1 POST /api/v1/system/auth/login

Files involved:

- internal/modules/system/routes.go
- internal/core/auth/provider_sqlc.go
- internal/core/auth/user_repository.go
- internal/core/storage/postgres_store.go

Runtime path:

1. route handler receives login payload
2. handler calls goAuth engine Login
3. goAuth asks StoreUserProvider for user by identifier
4. provider calls auth repository
5. repository executes relational query operation via store
6. store executes against pgx runner
7. result maps back to goAuth user record
8. goAuth issues tokens

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

## 11. Behavioral Changes After Store-First Redesign

What changed:

- no service-level dependency on query helper wrappers
- auth persistence now follows repository + store architecture
- transaction orchestration unified at store layer
- runtime exposes generic store surfaces to modules

What did not change:

- module registration model
- route policy system
- goAuth integration boundary
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
- [docs/auth-goauth.md](auth-goauth.md)
- [docs/workflows.md](workflows.md)
- [docs/environment-variables.md](environment-variables.md)
