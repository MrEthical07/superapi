# Overview

SuperAPI is a production-grade Go API template designed for teams that want a strong architecture baseline from day one.

This page is intentionally beginner-friendly. If you are new to this repository, read this first, then continue into the deeper docs linked at the end.

## 1. What This Template Gives You

Out of the box, SuperAPI provides:

- A module-based API structure so features are isolated and easy to maintain.
- A policy system for route behavior (auth, tenant, RBAC, rate-limit, cache, cache-control).
- A standard response envelope and typed application errors.
- Built-in goAuth integration for login, refresh, and protected routes.
- Redis-backed response caching and rate limiting.
- Observability primitives (metrics, tracing, structured logs).
- An sqlc-based relational data layer with clear, enforced boundaries.

In short: it gives you a "production-ready skeleton" where architecture rules are enforced, not optional.

## 1a. Why This Template (Problems It Solves)

Standing up a production Go SaaS backend means solving the same easy-to-get-wrong
problems every time. SuperAPI solves them and enforces the solution:

- **Auth is a lifecycle, not a login handler.** goAuth v0.4.0 gives you refresh, graceful logout, remember-me, MFA-aware login, session ceilings, key rotation, and abuse limiting — not a JWT snippet you grow yourself.
- **Cache/rate-limit keys are a footgun.** Keying is declared per route (explicit `VaryBy`/scope + tag invalidation), so you don't leak one user's cached response to another.
- **Multi-tenancy is risky to retrofit.** It lives behind one `TENANCY_ENABLED` flag with clean seams: zero cost when off, first-class isolation when on, deletable if never needed.
- **Data-access discipline erodes.** One enforced flow (Service → Repository → sqlc → pgx) is checked by a static verifier that fails the build on violations — there is no second pattern to drift toward.
- **Misconfiguration ships silently.** Startup linting rejects unsafe/contradictory config before the server takes traffic.
- **Observability is always "later".** Metrics, tracing, and structured logs are wired in from day one.
- **Templates lock you in.** Every optional subsystem disables by config (zero code) or deletes cleanly — see [trim-to-what-you-need.md](trim-to-what-you-need.md).

**What makes it good:** one enforced data-layer architecture (not two), policy-ordered
middleware with fail-fast static verification, secure-by-default production paths, a
real auth engine, and features that are genuinely optional in both directions. It is a
*template* (a snapshot, no auto-updates) and pre-1.0 by intent — port upgrades manually
and validate with tests and the verifier.

## 2. The Core Mental Model

There are two core flows you should remember.

### 2.1 Request flow

Client Request
-> Global middleware
-> Route-level policies
-> Handler
-> Service
-> Repository
-> sqlc queries
-> pgx (pool or transaction)
-> Response envelope

### 2.2 Data flow

Service
-> Repository
-> sqlc queries
-> pgx (pool or transaction)

This second flow is the most important architecture rule in the repo.

## 3. Data Layer Rules In Plain English

The enforced architecture is:

Service -> Repository -> sqlc queries -> pgx (pool or transaction)

What each layer does:

- Handler:
	- HTTP only (decode request, call service, encode response)
	- No business logic, no DB access.
- Service:
	- Business workflow and validation logic.
	- Calls the repository only.
	- Opens write transactions via `DB().WithTx(...)`, but runs no queries.
- Repository:
	- Data-access logic and query composition.
	- Obtains generated queries via `DB().Queries(ctx)`.
	- Maps sqlc rows to domain models.
- Data-access boundary (`storage.Postgres`):
	- Hands out sqlc queries bound to the active transaction or the pool.
	- Owns the transaction lifecycle; has no query surface of its own.

Hard constraints:

- Services must not run queries directly.
- Repositories must not manage transactions.
- Handlers must not call the DB.
- One storage type per module (relational, or the optional document store).

## 4. Current Backend Status

As of now:

- The relational data layer (sqlc over pgx) is fully wired through Postgres
  startup initialization.
- An optional, self-contained document (NoSQL) store is available for modules
  that need it (`internal/storage/document`), excluded from the binary until
  imported.
- Auth persistence goes through the repository + sqlc data layer like any module.

## 5. Built-In Routes You Can Test Immediately

| Method | Path | Purpose |
|---|---|---|
| GET | /healthz | Process liveness check |
| GET | /readyz | Dependency readiness check |
| GET | /metrics | Prometheus metrics endpoint |
| POST | /system/parse-duration | Simple demo endpoint for typed handler flow |
| POST | /api/v1/system/auth/login | Login through goAuth engine |
| POST | /api/v1/system/auth/refresh | Token refresh through goAuth |
| GET | /api/v1/system/whoami | Protected endpoint returning principal info |

## 6. What Makes This Different From "Basic CRUD Templates"

Many templates provide basic folders but leave architecture optional.

SuperAPI enforces behavior through:

- Route validation for policy order and safety.
- Startup config linting (invalid combinations fail fast).
- Shared response/error semantics.
- Dependency wiring that prevents bypassing architecture by default.

This means fewer hidden regressions as your codebase grows.

## 7. Beginner-Friendly First Steps

If this is your first day with the repo, follow this sequence:

1. Run the API in minimal mode.
2. Hit health endpoints and one system endpoint.
3. Read module author guide and inspect one existing module.
4. Generate a new module with module scaffolder.
5. Add one read route and one write route using the service -> repository -> `Queries(ctx)` flow.
6. Add policies in correct order and run verifier/build/tests.

## 8. Quick Start Modes

### 8.1 Minimal mode

Use this when you only want API process and no external dependencies yet.

- Postgres disabled
- Redis disabled
- Auth/cache/rate-limit disabled

### 8.2 Full mode

Use this when you want realistic behavior.

- Postgres enabled
- Redis enabled
- Auth enabled
- Rate-limit and cache enabled

See environment docs for exact variables and defaults.

## 9. Where New Contributors Usually Get Confused

Common confusion points:

- "Can I run queries from the service?"
	- No. The service calls the repository only; it may open a `WithTx` boundary.
- "Can the repository return sqlc/pgx rows?"
	- No. The repository returns domain models/errors.
- "Should reads always use transactions?"
	- No. Write paths use `WithTx`; reads are direct unless there is a specific requirement.
- "Can one module support SQL and document at once?"
	- No. Pick one storage type per module.

## 10. Documentation Path

Read these in order:

1. [docs/architecture.md](architecture.md)
2. [docs/modules.md](modules.md)
3. [docs/module_guide.md](module_guide.md)
4. [docs/crud-examples.md](crud-examples.md)
5. [docs/transactions.md](transactions.md)
6. [docs/auth-goauth.md](auth-goauth.md)
7. [docs/workflows.md](workflows.md)
8. [docs/environment-variables.md](environment-variables.md)

## 11. Final Takeaway

If you remember only one thing, remember this:

SuperAPI is not just "Go folders + helpers". It is an enforced architecture where route behavior, dependency wiring, and data-access boundaries are designed to stay maintainable as your API grows.
