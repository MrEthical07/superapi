## What’s done so far

* Bootstrap + repo hygiene
* Core app lifecycle + graceful shutdown
* chi router
* structured logging
* config-driven global middleware assembly
* typed JSON handler adapter + validation hook
* Postgres + Redis wiring
* real readiness checks

## What’s still remaining (big picture)

There are **4 major layers** still to build before this becomes your reusable “just add module” API template:

### 1) Data-layer developer experience

You’re starting this now with Task 06.
Still needed after that:

* migration runner binary/command (safe operational workflow)
* tx helper patterns (per-request/service transaction ergonomics)
* module DB conventions (where queries/repos live)
* optional test DB harness (later)

### 2) Module framework maturity

You have module registration, but still need the “easy module authoring” experience:

* module scaffolder/template (optional but high leverage)
* route grouping/versioning conventions (`/api/v1/...`)
* module deps access pattern (clean `Deps` usage)
* standardized DTO/repo/service conventions doc

### 3) Security + policy layer (on hold for now)

This is your biggest differentiator after infra:

* **policy engine**
* auth policy (goAuth adapter)
* RBAC/permission policy
* rate-limit policy
* cache read/invalidate policy (route-level TTL/key control)
* tenant scope policy
* idempotency policy (optional but great for SaaS billing/payments)

### 4) Ops/observability hardening

Still missing for “real-world-ready at scale”:

* metrics (Prometheus)
* request access logging (with sampling)
* tracing hooks (OTel, config-driven)
* pprof/profiling gate (protected/non-prod)
* runbook docs and incident toggles

---

# Recommended next task sequence (practical path)

Here’s the sequence I’d recommend **before** policy system:

## Phase A — Finish data layer baseline

1. **Task 06:** migrations + sqlc baseline ✅ (next) --> DONE
2. **Task 07:** transaction helper + module data access conventions --> DONE
3. **Task 08:** migration runner command (`cmd/migrate`) + Makefile/script targets hardening --> DONE

## Phase B — Build one real module end-to-end

4. **Task 09:** example `users` (or `tenants`) module using:

   * typed handler adapter
   * pgx/sqlc queries
   * validation
   * structured errors
   * readiness-safe dependency usage

This becomes your **reference implementation** for future modules.

## Phase C — Observability and operational safety

5. **Task 10:** metrics baseline (Prometheus)
6. **Task 11:** request access logging middleware (structured + sampling + exclusions like `/healthz`)
7. **Task 12:** request timeout semantics alignment (server timeouts vs middleware timeout behavior)

## Phase D — Then policy system (security-first)

8. **Task 13:** policy engine core
9. **Task 14:** auth policy adapter (goAuth integration)
10. **Task 15:** rate-limit policy (Redis-backed)
11. **Task 16:** cache policy (route-level keys/TTL/invalidation)
12. **Task 17:** tenant scope + RBAC policy helpers

---

# What specifically is still missing (detailed checklist)

## Core / platform

* [ ] `Deps` finalized and documented (likely partially done now)
* [ ] route grouping/versioning convention (`/api/v1`)
* [ ] request timeout semantics (global middleware vs server timeouts)
* [ ] standardized error codes catalog (expand beyond current set)

## Data

* [ ] migrations tooling + docs (**Task 06**)
* [ ] sqlc config + generated package conventions (**Task 06**)
* [ ] transaction helper (`WithTx`) and query binding helpers
* [ ] migration command/binary
* [ ] module SQL/query file conventions

## Modules / DX

* [ ] example real module (users/tenants)
* [ ] module guide doc
* [ ] optional module scaffolding generator (`make module name=...`)

## Redis usage beyond wiring

* [ ] cache manager abstractions (not policies yet if you want)
* [ ] key versioning/tag invalidation strategy implementation
* [ ] shared key builders (tenant/user/query)

## Security / policy layer (later)

* [ ] policy engine abstraction
* [ ] auth policies via goAuth
* [ ] RBAC permission checks
* [ ] rate limiting policies
* [ ] cache policies
* [ ] tenant scope policies
* [ ] idempotency policies (optional but useful)
* [ ] security headers/CORS finalization (based on product needs)
* [ ] CSRF strategy if cookie auth is used in some projects

## Observability / ops

* [ ] Prometheus metrics
* [ ] request logging middleware
* [ ] tracing hooks (OTel)
* [ ] pprof and profiling guidance
* [ ] runbook docs / incident toggles
* [ ] deployment docs (readiness/liveness expectations)

## Testing / quality

* [ ] integration test harness for DB/Redis (containerized or local optional)
* [ ] load test scripts (k6/vegeta) for 10k RPS target scenarios
* [ ] benchmark tests for hot path (router + adapter + middleware chain)

---