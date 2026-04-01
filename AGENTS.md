# AGENTS.md

## 1. Overview

This repository is a production-grade Go API template for SaaS backends.

It is structured around explicit modules, explicit dependency wiring, and policy-based route behavior.

How agents should interact with this repo:
- Follow existing patterns before introducing new abstractions.
- Prefer small, explicit changes in module scope.
- Treat policy validation and startup linting as hard constraints.

## 2. Core Architecture Rules

- The runtime bootstrap entrypoint is `cmd/api/main.go`.
- App wiring lives in `internal/core/app`.
- Modules are registered in `internal/modules/modules.go`.
- Module internals must follow handler/service/repo separation.
- Policies are mandatory for behavioral guarantees (auth, tenant, RBAC, rate-limit, cache).

## 3. How to Add a New Feature (PRIMARY WORKFLOW)

1. Create a new module.
2. Define DTOs.
3. Implement handler.
4. Implement service logic.
5. Implement repo queries.
6. Register routes with proper policies.

Implementation references:
- `docs/modules.md`
- `docs/crud-examples.md`
- `docs/module_guide.md`

## 4. Route Creation Rules (VERY IMPORTANT)

Always attach policies explicitly when route behavior requires auth/isolation/control.

Required policy order:
1. auth
2. tenant
3. rbac
4. rate limit
5. cache

Do not bypass `policy.MustValidateRoute` / validator-backed route checks.

Reference:
- `internal/core/policy`
- `docs/policies.md`

## 5. Policy Usage Guidelines

- Use `AuthRequired` for all protected routes.
- Use tenant policies (`TenantRequired`, `TenantMatchFromPath`) for tenant-scoped endpoints.
- Use `RequirePerm` / `RequireAnyPerm` for RBAC constraints.
- Use rate limits on abuse-prone endpoints (auth, write-heavy, expensive reads).
- Use cache only with safe vary dimensions (`VaryBy.UserID` or `VaryBy.TenantID` when authenticated).

References:
- `docs/policies.md`
- `docs/cache-guide.md`

## 6. Auth Integration Rules

- Use the goAuth provider integration in `internal/core/auth`.
- Do not reimplement token parsing/validation in modules.
- Keep auth mode behavior explicit (`jwt_only`, `hybrid`, `strict`).

Reference:
- `docs/auth-goauth.md`
- `docs/authDocs/`

## 7. Cache Usage Rules

- Define route tags and TTL intentionally.
- Use `VaryBy` explicitly for identity-sensitive responses.
- Never cache authenticated responses without identity-safe key variation.
- Invalidate tags on successful writes.

Reference:
- `docs/cache-guide.md`
- `docs/policies.md`

## 8. Data Layer Rules

- Use sqlc-generated query access from `internal/core/db/sqlcgen` via wrappers.
- Do not write raw SQL in handlers.
- Keep DB access in repo/service layer, not transport layer.
- Follow migration + schema + queries + generate workflow.

Reference:
- `docs/workflows.md`
- `docs/module_guide.md`

## 9. Performance Rules

- Keep hot path lean (middleware and policy execution).
- Avoid unnecessary Redis calls and high-cardinality keys.
- Avoid heavy middleware for routes that do not need it.
- Validate hot-path changes with benchmarks and load scripts.

Reference:
- `docs/performance-testing.md`

## 10. What NOT to Do

- Do not bypass policy layers for protected routes.
- Do not put business logic in handlers.
- Do not duplicate authentication logic.
- Do not modify core infrastructure without a clear need and validation.
- Do not edit generated sqlc files under `internal/core/db/sqlcgen/`.

## 11. Where to Look for Docs

- auth: `docs/auth-goauth.md`, `docs/authDocs/`
- cache: `docs/cache-guide.md`
- modules: `docs/modules.md`, `docs/crud-examples.md`, `docs/module_guide.md`
- performance: `docs/performance-testing.md`
- environment/runtime: `docs/environment-variables.md`
- workflows: `docs/workflows.md`

## 12. Modification Rules

- Prefer extending modules over changing core.
- If core must change, keep changes explicit and minimal.
- Preserve constructor + method patterns and existing public contracts.
- Update docs when behavior or configuration changes.

Always prefer:
- existing patterns over new abstractions
- policy-based solutions over custom logic

Never:
- duplicate authentication logic
- bypass validation layers
- introduce global state

Decision rules:
- If task involves authentication, refer auth docs.
- If task involves caching, refer cache guide.
- If task involves database changes, use sqlc patterns.
