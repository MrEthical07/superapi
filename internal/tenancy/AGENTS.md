# AGENTS.md (internal/tenancy)

Read this before changing the tenancy feature. It adds to the repository-wide
AGENTS.md; the architecture rules there still apply. Behavior is documented in
docs/multi-tenancy.md; removal in docs/removing-tenancy.md.

## Ownership

- Everything tenancy owns lives in this directory or in a file whose name
  contains `tenancy` (migration, sqlc schema/queries and generated code,
  verifier and scaffolder extension files, docs, workflows, the registration
  file `internal/features/tenancy.go`). Nothing else may mention tenancy: core
  code, core docs and core tests describe generic mechanisms only, and CI
  proves `make init --no-tenancy` leaves no trace.
- The only tenancy markers outside this directory are the registration line in
  `internal/features/features.go` and the settings blocks in `.env.example` and
  `docs/environment-variables.md` (at most three tenancy marker blocks outside
  tenancy-owned files; keep it that way).

## Rules

- Tenancy is compiled in but off by default (TENANCY_ENABLED). When off the
  feature only contributes the principal's tenant attribute (goAuth's default
  tenant "0") and the route rules; do not add other behavior to the off path.
- Read the tenant with `RequestTenantFromContext` (request) or
  `PrincipalTenant` / `TenantIDFromContext` (principal); never from headers in
  module code. Only the resolver middleware and trusted tooling call
  `WithRequestTenant`.
- `AuthExtension` binds a token to the request tenant with tenancy on; tenant
  routes still need `TenantRequired` (and `TenantMatchFromPath` for
  `{tenant_id}` paths). Keep the rejection the same 401 as an invalid token.
- Scope tenant-owned queries by `tenant_id` in SQL (db/queries/tenancy.sql).
  Core queries never mention `tenant_id`; the column default covers inserts.
- `Provider` decorates `auth.StoreUserProvider`. Keep every optional goAuth
  interface the core provider implements (assert each at compile time). It adds
  no scoping to id-keyed calls (ConsumeBackupCode, WebAuthn list/remove): goAuth
  v0.6.2 and later resolves the user in the request tenant first, and
  `TestCrossTenantMFAIsRejected` and `TestCrossTenantWebAuthnListAndRemoveAreRejected`
  prove it. Do not downgrade goAuth below v0.6.2; if a goAuth method stops
  scoping, those tests fail and the scoping must come back here.
- Password writes are the exception: `Provider` implements
  `goauth.TenantAwarePasswordUpdater` (goAuth v0.7.0), so ChangePassword, reset
  confirm and rehash-on-login write `WHERE id = $1 AND tenant_id = $2` for the
  tenant goAuth resolved. Keep that assertion, and keep the not-found result
  for a user of another tenant; `TestPasswordWritesAreTenantScoped` and
  `TestCrossTenantPasswordWriteIsRejected` prove both.
- Tenant tests use `tenancytest` (in-memory store and a multi-tenant engine
  built the way the app builds it), not fakes of goAuth.
- Do not expose goAuth audit events to tenant users or tenant admins.
