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
- A store-first data layer architecture with clear boundaries.

In short: it gives you a "production-ready skeleton" where architecture rules are enforced, not optional.

## 2. The Core Mental Model

There are two core flows you should remember.

### 2.1 Request flow

Client Request
-> Global middleware
-> Route-level policies
-> Handler
-> Service
-> Repository
-> Store
-> Backend (Postgres/Document backend)
-> Response envelope

### 2.2 Data flow

Service
-> Repository
-> Store
-> Backend

This second flow is the most important architecture rule in the repo.

## 3. Data Layer Rules In Plain English

The enforced architecture is:

Service -> Repository -> Store -> Backend

What each layer does:

- Handler:
	- HTTP only (decode request, call service, encode response)
	- No business logic, no DB access.
- Service:
	- Business workflow and validation logic.
	- Calls repository only.
	- Chooses transaction boundaries for write paths.
- Repository:
	- Data access logic and query composition.
	- Maps storage rows/documents to domain models.
	- Calls store only.
- Store:
	- Execution and transaction mechanism.
	- Knows backend behavior, not domain behavior.

Hard constraints:

- Services must not call stores directly.
- Repositories must not call drivers directly.
- Handlers must not call DB/store.
- One storage type per module (relational or document).

## 4. Current Backend Status

As of now:

- Relational store is fully wired through Postgres startup initialization.
- Document store contracts exist and can be used by modules.
- A document no-op store implementation exists as a contract-safe placeholder.
- Auth persistence now goes through repository + store layers.

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
5. Add one read route and one write route using service -> repository -> store flow.
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

- "Can I call store from service?"
	- No. Service should call repository only.
- "Can repository return driver rows?"
	- No. Repository should return domain models/errors.
- "Should reads always use transactions?"
	- No. Write paths use transactions; reads are direct unless there is a specific requirement.
- "Can one module support SQL and document at once?"
	- No. Pick one storage type per module.

## 10. Documentation Path

Read these in order:

1. [docs/architecture.md](architecture.md)
2. [docs/modules.md](modules.md)
3. [docs/module_guide.md](module_guide.md)
4. [docs/crud-examples.md](crud-examples.md)
5. [docs/auth-goauth.md](auth-goauth.md)
6. [docs/workflows.md](workflows.md)
7. [docs/environment-variables.md](environment-variables.md)

## 11. Final Takeaway

If you remember only one thing, remember this:

SuperAPI is not just "Go folders + helpers". It is an enforced architecture where route behavior, dependency wiring, and data-access boundaries are designed to stay maintainable as your API grows.
