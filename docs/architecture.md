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
- internal/features
	- the one place that lists the optional features compiled into the binary
- internal/<feature>
	- one directory per optional feature, outside core (see section 13)
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
5. Load the optional features (each reads and lints its own settings and returns
   its hooks); parse auth mode.
6. If auth enabled:
	 - create the auth user repository over the `storage.Postgres` boundary
	 - create the sqlc-backed `StoreUserProvider` (WebAuthn credential
	   repository, and — with AUTH_TOTP_ENABLED — the MFA repository and TOTP
	   secret cipher); a feature may wrap it and the user repository
	 - create the goAuth engine (v0.6.0) with Redis + provider + auth feature
	   flags + the features' goAuth config mutators
	 - register the features' auth extensions on the engine
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
10. Feature middleware (only when a feature contributes one)
11. Router dispatch

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
- auth prerequisites for RBAC policies
- cache safety rules on authenticated routes (must vary by user or an
  identity-bearing key part)
- route rules contributed by optional features (`policy.RouteRule`), applied by
  the router and by `superapi-verify`

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

SuperAPI is on goAuth **v0.6.0**. The engine is built in
internal/core/auth/goauth_provider.go and receives a `goauth.UserProvider`.

Current provider implementation: internal/core/auth/provider_store.go
(`StoreUserProvider`). It also implements the TOTP and backup-code methods and
`WebAuthnCredentialProvider`. The provider is keyed by the user ids goAuth hands
it; an optional feature that scopes users wraps it with a decorator (section
13).

Provider path (sqlc data layer):

StoreUserProvider -> UserRepository / MFARepository -> storage.Postgres (sqlc queries) -> pgx

The repositories use `pg.Queries(ctx)` like any other repository and map
generated rows to storage-layer projections. goAuth configuration is set in
internal/core/auth/config.go. HTTP endpoints live in `internal/modules/auth`
(handler -> service -> engine). See [docs/auth-goauth.md](auth-goauth.md) and
[docs/auth-flows.md](auth-flows.md).

Optional features attach to this path through the hooks in section 13: a goAuth
config mutator, a provider decorator, feature middleware and auth extensions.

## 9. Route-Level Flow Examples

### 9.1 POST /api/v1/auth/login

Files involved:

- internal/modules/auth/routes.go, handler.go
- internal/modules/auth/service.go
- internal/core/auth/provider_store.go
- internal/core/auth/user_repository.go
- internal/core/storage/postgres_store.go

Runtime path:

1. route handler receives login payload and calls the module's service
2. the service calls the goAuth engine (`LoginWithOptions`, honoring remember-me)
3. goAuth asks StoreUserProvider for the user by identifier
4. provider calls the auth repository
5. repository runs `pg.Queries(ctx).GetAuthUserByLogin(...)` on the pool
   (a feature that scopes users decorates the provider)
6. the generated row maps back to a goAuth user record
7. goAuth issues tokens, or returns an MFA challenge if a second factor is required

### 9.2 POST /api/v1/auth/refresh

High-level path:

- handler calls goAuth refresh
- goAuth performs token/session validation
- provider/repository/store path is used when user persistence reads are required

### 9.3 GET /api/v1/auth/whoami

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

1. server shutdown with configured timeout (`HTTP_SHUTDOWN_TIMEOUT`)
2. drain the notification dispatcher: it stops accepting messages, then waits
   for password-reset / email-verification deliveries already in flight, for at
   most what is left of the shutdown timeout. Deliveries still running at the
   deadline are abandoned (told to stop through their context) and the count is
   logged; a notifier that ignores its context cannot block exit. This runs
   after the HTTP server has stopped, so no handler can queue another message,
   and before redis/postgres close, since a notifier may still use them
3. close redis
4. close postgres
5. shutdown tracing
6. close auth engine resources

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

## 13. Optional features

An optional feature is a capability that some projects want and others delete:
the template ships one (the package listed first in
[internal/features/features.go](../internal/features/features.go)) as the
reference implementation. Core never imports a feature. A feature lives in its
own package outside `internal/core`, loads and lints its own settings, and plugs
into core through a small set of plain hooks that are applied once at startup.
Nothing is registered globally at run time.

### 13.1 The registration point

`internal/features/features.go` returns the features compiled into the binary:

```go
func All() []app.Feature {
	return []app.Feature{
		myFeature(),
	}
}
```

`cmd/api` passes the list to `app.New(cfg, log, modules, features.All()...)`;
`cmd/createuser` passes it to `app.NewDependencies`. Adding a feature is one line
here; removing it is deleting the line and the feature's files. A feature is
*compiled in* once registered and *switched on* by its own settings.

### 13.2 The hooks

A feature implements `app.Feature`:

```go
type Feature interface {
	Name() string
	Load(core *config.Config) (*app.Hooks, error)
}
```

`Load` runs after core config is loaded and linted. It reads and lints the
feature's own environment and returns the hooks, or `nil` when the feature
contributes nothing for this configuration. Every field of `app.Hooks` is
optional:

| Hook | What it does | Where it is applied |
|---|---|---|
| Settings: `config.EnvString/EnvBool/EnvInt/EnvDuration/EnvCSV` inside `Load` | the feature loads its own keys instead of adding fields to `config.Config`; `superapi-verify` finds the keys wherever the package lives and still requires them in `.env.example` and `docs/environment-variables.md` | `Load` |
| `Deprecations` | startup warnings for retired keys read with `config.EnvDeprecated` (documented, but exempt from `.env.example`) | `app.New` logs them |
| `GoAuthConfig` (`auth.ConfigMutator`) | switches on the goAuth settings the feature owns | `auth.ProjectGoAuthConfig`, before goAuth's lint |
| `UserProvider` | wraps the core `*auth.StoreUserProvider` (a decorator) | `initDependencies`, before the engine is built |
| `UserRepository` | wraps the core `auth.UserRepository` modules receive as `Dependencies.AuthUsers` | `initDependencies` |
| `Middleware` | global middleware at a fixed position: innermost, after RequestID/ClientIP/recovery/CORS/headers/limits/tracing/access log, just before routing | `httpx.WithFeatureMiddleware` |
| `AuthExtension` (`policy.AuthExtension`) | `Attribute` adds a principal attribute (`auth.AuthContext.Attributes`, read with `principal.Attribute(key)`); `Check` runs after goAuth accepted the token and rejects with the same 401 as an invalid token | `policy.UseAuthExtensions(engine, ...)`, read by every `policy.AuthRequired` on that engine |
| `RouteRules` (`policy.RouteRule`) | extra checks on every registered route | `Mux.UseRouteRules` |

Rules the hooks rely on:

- **A provider decorator must keep every optional goAuth interface.** goAuth
  detects capabilities by type assertion (for example
  `WebAuthnCredentialProvider`), so a wrapper that hides one silently disables
  it. Embed the core provider (`*auth.StoreUserProvider`) so every method is
  promoted, override only what the feature must change, and add a compile-time
  assertion (`var _ goauth.WebAuthnCredentialProvider = (*Provider)(nil)`) for
  each interface the core provider satisfies. At most one feature may wrap the
  provider.
- **Attributes carry feature data.** Core has no feature-specific principal
  fields. `whoami` reports each attribute as an extra top-level field under its
  own key, so a feature that is not registered adds nothing.
- **Cache and rate limits use generic parts.** `cache.KeyPart{Name, Extract,
  Identity}` goes on `cache.CacheVaryBy.Parts` (a key component) and
  `cache.CacheTagSpec.Parts` (an invalidation tag component). `Identity: true`
  marks a part that names who the response is for; the safety rule for cached
  authenticated responses is "vary by `UserID` **or** an identity-bearing
  part". A tag part that resolves to an empty value is an error, never an
  unscoped tag. Rate limits use `ratelimit.Rule{Scope, Keyer}` with a
  feature-defined `Scope` and `Keyer`.
- **Policies annotate themselves.** A feature policy is registered with
  `policy.Annotate(p, policy.Metadata{Type, Name, Stage, Data})`. `Stage` places
  it in the order (`policy.StageIsolation` sits between auth and RBAC) and
  `Data` carries what route rules need. Policies are keyed by their function
  value, so each distinct policy needs its own function literal.
- **Presets for a feature** build their chain from `policy.ResolvePreset(opts...)`
  and validate it with `policy.MustValidatePreset`.
- **Commands and tools extend through their own seams.** `app.UserCLI` adds flags
  and steps to `cmd/createuser`; `modulegen.RegisterExtension` adds a scaffolding
  option; `validator.RegisterExtension` teaches `superapi-verify` the feature's
  policies, identity-bearing cache parts, rules and hints. The last two register
  from an `init` in a file of the feature's own (they are build-time tools, not
  part of the running server).

### 13.3 Ownership and removal

A feature owns everything that mentions it, so removing it is mechanical:

- its package directory (`internal/<feature>/`), including its tests and any
  test helper package;
- files elsewhere whose **name contains the feature name**: its migration
  (`db/migrations/NNNNNN_<feature>.*`), sqlc schema mirror (`db/schema/<feature>.sql`),
  queries (`db/queries/<feature>.sql`) and generated code, the verifier and
  scaffolder extension files, its docs (`docs/<feature>.md`,
  `docs/removing-<feature>.md`), its workflow;
- one registration line in `internal/features/features.go`, plus its own
  settings block in `.env.example` and `docs/environment-variables.md`, each
  wrapped in the feature's template marker comments (see `cmd/templateinit`).

`cmd/templateinit` prunes a feature by deleting the directory and every file
named after it (`feature.NameContains`) and stripping the marked blocks. CI runs
that on a copy and requires the pruned project to build, vet, lint, verify and
pass its tests with no mention of the feature left. Core code, docs and tests
outside those files must therefore never name a feature; describe the generic
mechanism and let the feature's own docs describe the feature.

### 13.4 Adding a feature

1. Create `internal/<feature>/` with a type implementing `app.Feature` whose
   `Load` reads and lints its settings and returns only the hooks it needs.
2. Put SQL in `db/migrations/NNNNNN_<feature>.*`, `db/schema/<feature>.sql` and
   `db/queries/<feature>.sql`; run `make sqlc-generate`. Core queries must not
   mention the feature's columns.
3. Add its settings to `.env.example` and `docs/environment-variables.md` in a
   marked block.
4. Register it in `internal/features/features.go` (one line, in a marked block).
5. Add `docs/<feature>.md` and `docs/removing-<feature>.md`, and a `NameContains`
   entry in `cmd/templateinit/features.go` so `--no-<feature>` exists.
6. Keep a test that proves the feature's isolation claims (what it rejects, what
   it leaves alone when switched off) next to the code.

A minimal feature that adds a region to the principal and to cached responses:

```go
package regions

type Feature struct{}

func (Feature) Name() string { return "regions" }

func (Feature) Load(core *config.Config) (*app.Hooks, error) {
	if !config.EnvBool("REGIONS_ENABLED", false) {
		return nil, nil // as if it were not compiled in
	}
	return &app.Hooks{
		AuthExtension: &policy.AuthExtension{
			Attribute: func(r *goauth.AuthResult) (string, string) { return "region", regionOf(r.UserID) },
		},
	}, nil
}

// CacheVary is what modules put in VaryBy.Parts.
func CacheVary() cache.KeyPart {
	return cache.KeyPart{
		Name:     "region",
		Identity: true,
		Extract:  func(_ *http.Request, p auth.AuthContext) string { return p.Attribute("region") },
	}
}
```

## 14. Related Docs

- [docs/overview.md](overview.md)
- [docs/modules.md](modules.md)
- [docs/module_guide.md](module_guide.md)
- [docs/crud-examples.md](crud-examples.md)
- [docs/transactions.md](transactions.md)
- [docs/auth-goauth.md](auth-goauth.md)
- [docs/document-store.md](document-store.md)
- [docs/workflows.md](workflows.md)
- [docs/environment-variables.md](environment-variables.md)
