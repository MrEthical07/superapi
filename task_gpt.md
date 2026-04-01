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
* [x] load test scripts (k6/vegeta) for 10k RPS target scenarios
* [x] benchmark tests for hot path (router + adapter + middleware chain)

---

Next:

# 🧱 SUPERAPI HARDENING ROADMAP (ENFORCEMENT + DX)

---

# 🔴 TASK 1 — Policy Validation Engine (Runtime Enforcement)

## 🎯 Goal

Make it **impossible to register unsafe routes**.

---

## ❗ Problem

Right now:

* Policy order matters but isn’t enforced
* Missing policies = silent security bugs
* Misconfiguration is only documented

---

## ✅ What to build

### 1.1 Add validation layer inside router

Hook into:

```go
r.Handle(method, pattern, handler, policies...)
```

Before registration:

```go
validateRoute(method, pattern, policies)
```

---

### 1.2 Validation rules (STRICT)

Implement these checks:

#### 🔐 Auth rules

* If any of:

  * `RequirePerm`
  * `RequireRole`
  * `TenantRequired`

  👉 Then `AuthRequired` MUST exist

---

#### 🧱 Policy order rules

Enforce exact order:

1. AuthRequired
2. TenantRequired / TenantMatch
3. RBAC (Role/Perm)
4. RateLimit
5. CacheRead / CacheInvalidate

👉 If wrong → panic

---

#### 🏢 Tenant rules

If route:

* uses `TenantMatchFromPath`
  👉 MUST also include `TenantRequired`

---

#### 🧠 Cache safety rules

For `CacheRead`:

If route has `AuthRequired`:

* MUST have one of:

  * `VaryBy.UserID`
  * `VaryBy.TenantID`
  * `AllowAuthenticated=true`

Else:
👉 panic

---

#### ✍️ Write route rules

For methods:

* POST / PUT / PATCH / DELETE

If route has:

* corresponding GET with cache tags

👉 MUST include `CacheInvalidate` with same tag

(Keep simple first: warn or panic if no invalidate)

---

### 1.3 Failure behavior

👉 Always:

```go
panic("invalid route configuration: ...")
```

No logs. No warnings.

---

## 📁 Files to create

```
internal/core/policy/validator.go
internal/core/policy/validator_rules.go
```

---

## 🧪 Tests

* invalid order → panic
* missing auth → panic
* unsafe cache → panic
* valid route → passes

---

## 📚 Docs update

Update:

* `docs/policies.md`

Add section:

```
## Policy Validation (Strict Mode)

- Routes are validated at startup
- Invalid configurations will panic
- This guarantees security invariants
```

---

## ⚙️ Best implementation approach

* Represent policies as identifiable types (not anonymous funcs)
* Use type assertions or wrapper structs:

```go
type PolicyMeta struct {
    Type PolicyType
}
```

---

---

# 🟠 TASK 2 — Policy Presets (Reduce Cognitive Load)

## 🎯 Goal

Stop repeating policy chains. Reduce human error.

---

## ❗ Problem

Every route manually defines:

* auth
* tenant
* rbac
* rate limit
* cache

👉 Too much room for mistakes.

---

## ✅ What to build

Create preset builders:

### 2.1 Core presets

```go
policy.TenantRead(opts...)
policy.TenantWrite(opts...)
policy.PublicRead(opts...)
```

---

### 2.2 Example

```go
r.Handle(GET, "/projects/{id}", handler,
    policy.TenantRead(
        policy.WithCache(30*time.Second, "project"),
    ),
)
```

---

### 2.3 Preset internals

Each preset returns:

```go
[]policy.Policy
```

Example:

TenantRead:

* AuthRequired
* TenantRequired
* RateLimit (tenant scoped)
* CacheRead (safe defaults)

---

### 2.4 Config options

Use functional options:

```go
policy.WithCache(ttl, tags...)
policy.WithRateLimit(limit, window)
policy.WithStrictAuth()
```

---

## 📁 Files

```
internal/core/policy/presets.go
internal/core/policy/options.go
```

---

## 🧪 Tests

* preset generates correct order
* preset passes validator

---

## 📚 Docs update

Update:

* `docs/policies.md`

Add:

```
## Policy Presets

Recommended usage:
- TenantRead
- TenantWrite
...
```

---

## ⚙️ Best approach

* Build presets ON TOP of existing policies (no rewrite)
* Keep them composable but opinionated

---

---

# 🟡 TASK 3 — Unsafe Config Elimination

## 🎯 Goal

Remove all “silent safe fallbacks”

---

## ❗ Problem

Current system:

* silently bypasses unsafe cache
* allows missing tenant checks
* allows weak configs

---

## ✅ What to change

### 3.1 CacheRead behavior

Current:

> bypass if unsafe

Change:

```go
panic("unsafe cache config: missing vary-by for authenticated route")
```

---

### 3.2 Tenant enforcement

If route path contains:

```
{tenant_id}
```

👉 auto-require:

* TenantMatchFromPath
  OR
* panic

---

### 3.3 Auth enforcement

If RBAC exists without auth:
👉 panic (already in validator)

---

## 📁 Files

Modify:

```
internal/core/policy/cache.go
internal/core/policy/auth.go
```

---

## 🧪 Tests

* unsafe cache → panic
* missing tenant policy → panic

---

## 📚 Docs update

Add section:

```
## Safe-by-default guarantees

SuperAPI will refuse to start if:
- cache is unsafe
- auth is missing where required
...
```

---

---

# 🟢 TASK 4 — Static Analyzer (CI Enforcement)

## 🎯 Goal

Catch issues BEFORE runtime.

---

## ❗ Problem

Runtime panic is good — but CI should fail earlier.

---

## ✅ What to build

### 4.1 CLI tool

```bash
superapi verify ./...
```

---

### 4.2 What it checks

* Parse AST of `routes.go`
* Extract:

  * route
  * policies

Run same validation logic as runtime

---

### 4.3 Output

```
[ERROR] projects.routes.go:45
Missing TenantRequired for route /api/v1/tenants/{tenant_id}/projects
```

---

## 📁 Files

```
cmd/superapi-verify/main.go
internal/tools/validator/*
```

---

## 🧪 Tests

* broken module → fails
* valid module → passes

---

## 📚 Docs update

Update:

* `docs/workflows.md`

Add:

```bash
make verify
```

---

## ⚙️ Best approach

* reuse validation logic from runtime
* don’t duplicate rules

---

---

# 🔵 TASK 5 — Handler Unification (Remove Dual Pattern)

## 🎯 Goal

Eliminate split between typed and manual handlers

---

## ❗ Problem

Current:

* typed handler → no path params
* manual handler → messy

---

## ✅ What to build

### 5.1 New handler signature

```go
func(ctx *httpx.Context, req Request) (Response, error)
```

---

### 5.2 Context provides:

```go
ctx.Param("id")
ctx.Query("limit")
ctx.Header("X")
ctx.Auth()
```

---

### 5.3 Replace:

* `httpx.JSON`
* manual handlers

👉 with single adapter

---

## 📁 Files

```
internal/core/httpx/context.go
internal/core/httpx/adapter.go
```

---

## 🧪 Tests

* JSON decode works
* path params accessible
* validation works

---

## 📚 Docs update

Update:

* `docs/modules.md`
* `docs/crud-examples.md`

Remove dual handler explanation

---

## ⚙️ Best approach

* wrap `http.Request`
* inject parsed values once
* keep zero allocations where possible

---

---

# 🟣 TASK 6 — Config Profiles (Reduce Setup Friction)

## 🎯 Goal

Spin up new project with minimal config

---

## ❗ Problem

Too many env vars → friction

---

## ✅ What to build

### 6.1 Profiles

```bash
APP_PROFILE=dev
APP_PROFILE=prod
APP_PROFILE=minimal
```

---

### 6.2 Profile presets

Each sets defaults:

#### minimal

* no auth
* no cache
* no rate limit

#### dev

* auth=jwt_only
* cache enabled
* rate limit relaxed

#### prod

* auth=strict
* cache enabled
* rate limit strict

---

### 6.3 Override logic

Env vars override profile

---

## 📁 Files

```
internal/core/config/profile.go
```

---

## 🧪 Tests

* profile loads correct defaults
* overrides work

---

## 📚 Docs update

Update:

* `docs/environment-variables.md`

Add:

```
## Profiles
```

---

## ⚙️ Best approach

* apply profile BEFORE env parsing
* then override with explicit env

---

# 🧾 FINAL PRIORITY ORDER

Do in this order:

1. 🔴 Policy validation engine (foundation)
2. 🟡 Unsafe config elimination
3. 🟠 Policy presets
4. 🔵 Handler unification
5. 🟣 Config profiles
6. 🟢 Static analyzer (final polish)

---