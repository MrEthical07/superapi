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

- Services must call repositories for all data operations.
- Services may call store.WithTx(...) only to define transaction boundaries for write operations.
- Services must not call store execution methods (Execute, Query, etc.) directly.
- Repositories must call stores only.
- Repositories must not call database drivers directly.
- Repositories own all data access logic and must not control transaction boundaries.
- Store interfaces are execution-only and must not encode domain semantics.
- Store interfaces must not expose generic CRUD/query-language APIs as the module contract.
- Repositories own query logic and storage-model to domain-model mapping.
- Store implementations must remain unaware of domain structures.
- One storage type per module; do not mix relational and document logic inside one module.
- No direct DB access in handlers.

## 4. Transaction Rules

- Transaction API is mandatory at the store layer.
- Services own the write transaction boundary via store.WithTx(...); all store.Execute calls happen inside that scope through repository methods.
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

## 11. How To Add A New Feature (Primary Workflow)

Use this sequence for most feature work:

1. Define behavior first:
- endpoint shape (request/response)
- auth/tenant/rbac requirements
- cache/rate-limit requirements

2. Add or update module code in this order:
- dto.go (transport contract)
- handler.go (HTTP only)
- service.go (business workflow)
- repo.go (query + mapping logic)

3. Keep data flow enforced:
- handler -> service -> repository -> store

4. Register routes and policies explicitly in routes.go.

5. Validate and test:
- go test ./...
- go build ./...
- go run ./cmd/superapi-verify ./...

6. Update documentation when behavior/config/API architecture changes.

## 12. How To Add A New Module

Preferred scaffold command:

- make module name=projects

Optional scaffold flags:

- make module name=projects db=1
- make module name=projects auth=1 tenant=1 ratelimit=1 cache=1

Post-scaffold hardening checklist:

1. Confirm module registration in internal/modules/modules.go.
2. Refine generated service/repository contracts to domain-focused methods.
3. Wire one storage type only (relational or document) in module binding.
4. Ensure route policies follow required order.
5. Add tests for handler/service paths.

Do not ship scaffold defaults without architecture pass.

## 13. How To Add Data Storage For A Module

Choose one storage family per module:

- relational module -> storage.RelationalStore
- document module -> storage.DocumentStore

Required patterns:

- repository builds operations and calls store.Execute(...)
- service owns transaction boundary for writes via store.WithTx(...)
- reads are direct repository calls (no forced tx wrapper)

Relational path guidance:

1. Add migration files under db/migrations.
2. Mirror schema under db/schema.
3. Add/update query definitions under db/queries.
4. Regenerate code when required:
- make sqlc-generate

Constraint reminder:

- sqlc/driver objects are implementation details only
- never expose them in service/repository public interfaces

## 14. How To Add A New Backend Type (Core Change)

Only do this when necessary. Keep changes explicit and minimal.

Required steps:

1. Implement backend store in internal/core/storage:
- satisfy Store + TransactionalStore + backend-specific contract

2. Keep store domain-agnostic:
- no module domain structs in store implementation

3. Wire dependencies in internal/core/app/deps.go:
- init client/pool
- init store implementation
- set Dependencies.Store and typed store surface
- register readiness checks when applicable

4. Expose runtime access through internal/core/modulekit/runtime.go if needed.

5. Add tests and update architecture/docs pages.

Do not introduce compatibility layers that keep dual access patterns alive.

## 15. Route And Policy Checklist

For protected routes, policy order must be:

1. auth
2. tenant
3. rbac
4. rate limit
5. cache
6. cache-control (optional)

Safety checks:

- authenticated cache routes must vary by user or tenant
- tenant_id path routes must enforce tenant policies
- do not bypass route validator-backed registration

## 16. Performance Guidance

Hot-path principles:

- keep middleware/policy stacks minimal and intentional
- avoid high-cardinality cache/rate-limit key dimensions
- define cache TagSpecs and VaryBy scope deliberately
- prefer explicit invalidation scope over broad invalidation

Performance checks:

- make bench-hotpath
- make load-k6-10k (full profile)
- make load-vegeta-10k (token-based)

Always compare before/after numbers when changing hot paths.

## 17. Release And Publish Checklist

For release-impacting changes:

1. Update CHANGELOG.md with specific, verifiable entries.
2. Update release metadata in README.md.
3. Run quality gates:
- go test ./...
- go build ./...
- go run ./cmd/superapi-verify ./...
4. Ensure docs reflect the final runtime behavior.
5. Tag release after merge/publish prep:
- git tag -a vX.Y.Z -m "Release vX.Y.Z"
- git push origin vX.Y.Z
