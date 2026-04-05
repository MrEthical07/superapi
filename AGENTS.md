# AGENTS.md

## 1. Overview

This repository is a production-grade Go API template for SaaS backends.

The enforced architecture is:

Service -> Repository -> Store -> Backend

Use current code as reference, but do not preserve legacy SQL-centric patterns.

## 2. Core Architecture Rules

- Runtime bootstrap entrypoint is cmd/api/main.go.
- App wiring lives in internal/core/app.
- Modules are registered in internal/modules/modules.go.
- Module internals must follow handler/service/repository separation.
- Policies remain mandatory for behavioral guarantees (auth, tenant, RBAC, rate-limit, cache, cache-control).

## 3. Data Layer Rules (Hard Constraints)

- Services must call repositories only.
- Services must not call stores directly.
- Repositories must call stores only.
- Repositories must not call database drivers directly.
- Store interfaces are execution-only and must not encode domain semantics.
- Store interfaces must not expose generic CRUD/query-language APIs as the module contract.
- Repositories own query logic and storage-model to domain-model mapping.
- Store implementations must remain unaware of domain structures.
- One storage type per module; do not mix relational and document logic inside one module.
- No direct DB access in handlers.

## 4. Transaction Rules

- Transaction API is mandatory at the store layer.
- Services must use transaction scope only through repository workflows.
- Transactions apply to write paths only.
- Read paths must not be forced into transaction context.
- Backend-specific transaction behavior belongs only to store implementations.

## 5. Route Creation Rules

Always attach policies explicitly when route behavior requires auth/isolation/control.

Required policy order:
1. auth
2. tenant
3. rbac
4. rate limit
5. cache
6. cache-control (optional, after cache)

Do not bypass policy.MustValidateRoute / validator-backed route checks.

## 6. Auth Integration Rules

- Use goAuth integration in internal/core/auth.
- Keep goAuth user provider data-store independent from service/module layers.
- Auth persistence must go through auth repository + store contracts.
- Do not reimplement token parsing/validation in modules.
- Keep auth mode behavior explicit (jwt_only, hybrid, strict).

## 7. Cache Usage Rules

- Define TagSpecs and TTL intentionally.
- Use VaryBy for cache isolation and TagSpecs for freshness/invalidation scope.
- Never cache authenticated responses without identity-safe key variation.
- Invalidate matching TagSpecs on successful writes.

## 8. What Not To Do

- Do not bypass policy layers for protected routes.
- Do not put business logic in handlers.
- Do not expose sqlc or driver query objects to service/repository interfaces.
- Do not keep dual data-access patterns alive.
- Do not introduce module-level branching like if sql else mongo.
- Do not modify core infrastructure without clear need and validation.
- Do not edit generated files manually under internal/core/db/sqlcgen/.

## 9. Modification Rules

- Prefer existing patterns over introducing alternatives.
- If core changes are required, keep changes explicit and minimal.
- Preserve constructor and method contracts unless task explicitly changes them.
- Update docs when behavior or architecture changes.

Always prefer:
- one enforced architecture over compatibility layers
- policy-based solutions over custom middleware logic

Never:
- duplicate authentication logic
- bypass validation layers
- introduce global mutable state

## 10. Where To Look

- Architecture: docs/architecture.md
- Modules: docs/modules.md, docs/module_guide.md, docs/crud-examples.md
- Policies: docs/policies.md
- Cache: docs/cache-guide.md
- Auth: docs/auth-goauth.md, docs/authDocs/
- Runtime/config: docs/environment-variables.md, docs/workflows.md
