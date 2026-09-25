# Changelog

All notable changes to this template are documented in this file.

## v0.9.0 (unreleased)

goAuth v0.5.0, the full auth lifecycle, and a clone-ready template. Tenancy now
works end to end when enabled; every new auth feature is opt-in and off by
default, so a single-tenant, minimal clone behaves like v0.8.0 apart from the
changes listed first.

**Behavior changes to note when upgrading:**

- **Auth routes moved** from the `system` demo module to a dedicated `auth`
  module: `/api/v1/system/auth/{login,mfa/confirm,refresh,logout,webauthn/*}`
  are now `/api/v1/auth/...`, and `/api/v1/system/whoami` is
  `/api/v1/auth/whoami`. There are no aliases. Request/response shapes are
  unchanged. With `AUTH_ENABLED=false` the auth routes are no longer registered
  (404 instead of 503).
- **The `system` module is removed**, including `POST /system/parse-duration`
  and its unused `system_settings` schema. Migration `000001` stays for history
  but is a no-op on new databases.
- **`make run` now starts the server** (`./cmd/api`); it used to run a
  placeholder that printed `api-template: ok`. The root `main.go`/`doc.go` are
  gone.
- **`MAKEFILE` is renamed to `Makefile`**, so GNU make finds it on
  case-sensitive filesystems (Linux, CI). Make targets now source `.env`, and
  `migrate-*` default `DB_URL` to `POSTGRES_URL`.
- **`TENANCY_ENFORCE_ISOLATION` is deprecated and ignored**: goAuth v0.5.0 made
  `MultiTenant.EnforceIsolation` a no-op. It is still accepted (startup logs a
  warning) and no longer fails lint without `TENANCY_ENABLED`.
- **`AUTH_TEST_*` are refused unless `APP_ENV` is `dev` or `test`**: startup
  fails instead of silently switching JWT signing to a shared HS256 secret.
- **`TENANCY_ENABLED=true` now requires a tenant on every request** (except
  `/healthz`, `/readyz`, `/metrics`) and, with the default `TENANCY_VALIDATE=true`,
  a matching active `tenants` row. Before, the flag changed policy defaults but
  nothing attached a tenant, so every login landed in goAuth tenant `"0"`.
- **`account_version` now advances on every account-status change** and on
  TOTP enable/disable (migration 000006). goAuth revokes sessions stamped with
  an older version when the status changes, as designed.
- **`cmd/authgen` (`make auth`, `make auth-config`) is removed**; its
  regenerated schema would no longer match the provider. Use `make user` to
  create accounts.
- **`auth.NewGoAuthEngine` and `auth.ProjectGoAuthConfig` take an extra
  `auth.Features` argument**, and `auth.TenancySettings` no longer has
  `EnforceIsolation`.
- **`perftoken --create-if-missing` defaults to false**; `make perf-token` and
  the seed scripts pass it explicitly.
- Turning tenancy on changes where goAuth stores reset and verification records
  (request tenant instead of user tenant); links issued before the switch may
  not resolve. Drain them or let users re-request.

### Security

- **goAuth v0.5.0 cross-tenant fixes** (these apply only with
  `TENANCY_ENABLED=true`; single-tenant deployments were never affected):
  cross-tenant account takeover via password reset, cross-tenant login, and
  cross-tenant email verification. `StoreUserProvider` implements
  `goauth.TenantAwareUserProvider` with the tenant predicate in SQL, so goAuth
  can build with multi-tenancy on.
- **Tokens are bound to the request tenant**: `policy.AuthRequired` rejects a
  token whose tenant differs from the resolved tenant in every validation mode;
  `policy.TenantRequired` returns 404 on the same mismatch.
- **TOTP secrets are encrypted at rest** (AES-256-GCM, `AUTH_TOTP_ENCRYPTION_KEY`,
  bound to the user id). goAuth hands providers the raw secret.
- **Replay protection and single-use backup codes hold under concurrency**: the
  TOTP counter update only moves forward, and backup codes are consumed with a
  single conditional `UPDATE`.
- **Enumeration-safe account endpoints**: registration and reset/verification
  requests return the same 202 body whether or not the account exists; secrets
  are delivered out-of-band only and never appear in responses. Registration
  never accepts a role from the client.
- `cmd/createuser` never takes a password as an argument (hidden prompt or
  `--password-stdin`).
- `NOTIFY_LOG_SECRETS` is refused outside `APP_ENV=dev`.

### Added

- **goAuth v0.5.0** (from v0.4.0), including OpenTelemetry v1.44.0 via goAuth.
- **`internal/modules/auth`**: login, MFA confirm, refresh, logout, logout-all,
  sessions, password change, whoami, WebAuthn ceremonies, plus flag-gated
  groups: registration (`AUTH_REGISTRATION_ENABLED`, optional
  `AUTH_REGISTRATION_AUTO_LOGIN`), password reset
  (`AUTH_PASSWORD_RESET_ENABLED`), email verification
  (`AUTH_EMAIL_VERIFICATION_ENABLED`, `AUTH_EMAIL_VERIFICATION_REQUIRED`), and
  TOTP + backup codes (`AUTH_TOTP_ENABLED`, `AUTH_TOTP_ENCRYPTION_KEY`,
  `AUTH_TOTP_ISSUER`). Disabled groups are not registered. Handlers pass client
  IP and User-Agent to goAuth's limiters and audit trail.
- **TOTP and backup-code persistence**: migration `000006_auth_mfa`
  (`users.account_version`, `users.totp_enabled`, `user_totp`,
  `user_backup_codes`), `MFARepository`, and real provider methods replacing
  the stubs. `TOTP.RequireForLogin` follows `AUTH_TOTP_ENABLED`, so enrolled
  users are challenged at login.
- **Multi-tenancy**: migration `000005_users_tenant` (`users.tenant_id`, default
  `'0'`; global email uniqueness kept; no FK to `tenants`), tenant-scoped
  queries, and a tenant resolution middleware (`TENANCY_RESOLVER`
  header|subdomain, `TENANCY_HEADER`, `TENANCY_BASE_DOMAIN`, `TENANCY_VALIDATE`,
  `TENANCY_VALIDATE_CACHE_TTL`, `TENANCY_EXEMPT_PATHS`) that attaches the tenant
  with `goauth.WithTenantID`. Missing/malformed tenant -> 400; unknown or
  inactive -> 404.
- **`internal/core/notify`**: `Notifier` interface, no-op default, dev log
  driver (`NOTIFY_DRIVER`, `NOTIFY_LOG_SECRETS`, `NOTIFY_TIMEOUT`) and a bounded
  asynchronous dispatcher.
- **`cmd/createuser` / `make user`**: create accounts (first admin) through
  the configured engine, with `--tenant`/`--create-tenant`.
- **`cmd/templateinit` / `make init`**: rewrites the module path, resets
  README/CHANGELOG/LICENSE/SECURITY, strips template-maintainer content, prunes
  `--no-tenancy`, `--no-webauthn`, `--no-document-store`, `--no-devx`,
  `--no-perf`, `--no-demo` (or `--no-all`), then deletes itself. Idempotent,
  with `--dry-run` and `--keep-init`.
- **Local dev stack**: `docker-compose.yml` (Postgres 17 + Redis 7),
  `make dev-up`/`dev-down`/`dev-reset`, `make doctor`,
  `make test-integration`, and a multi-stage distroless `Dockerfile` (api,
  migrate, createuser).
- **Postgres integration tests** (`internal/core/db/dbtest`, enabled by
  `SUPERAPI_TEST_DATABASE_URL`) for tenant scoping and the MFA SQL.
- **Env documentation guard**: `internal/tools/envcheck`, run by
  `superapi-verify` and a test, fails when code reads an env var missing from
  `.env.example` or `docs/environment-variables.md`.
- **CI**: Postgres/Redis service containers, sqlc v1.30.0 drift check,
  `superapi-verify`, migrations up/down/up, gofmt, a `TENANCY_ENABLED=true` test
  pass, and a matrix job that runs `make init` (default and `--no-all`) on a
  copy and then that project's gate.
- POSIX ports of the Vegeta runner and k6 user seeding.
- `app.NewDependencies` / `Dependencies.Close` for command-line tools;
  `auth.WithRequestTenant` / `auth.RequestTenantFromContext`.

### Changed

- `ProjectGoAuthConfig` clears goAuth's pre-filled no-op
  `MultiTenant.TenantHeader`, so enabling tenancy lints clean.
- `cmd/perftoken` builds its engine with `app.NewDependencies` instead of a
  hand-rolled goAuth config (real role registry and JWT settings).
- `.env.example` lists every variable the code reads, grouped and commented,
  with no inline comments on active lines (works with `docker --env-file`).
- WebAuthn code moved into dedicated files (`provider_webauthn.go`,
  `config_webauthn.go`, `internal/modules/auth/webauthn.go`); behavior
  unchanged.
- Perf scenarios target `/api/v1/auth/*`; the former parse-duration share
  moved to whoami.

### Deprecated

- `TENANCY_ENFORCE_ISOLATION` (ignored; will be rejected in a future release).

### Removed

- The `system` module (`POST /system/parse-duration`) and the
  `system_settings` sqlc schema/queries/generated code.
- `cmd/authgen`, `authgen.example.yaml`, `make auth`, `make auth-config`.
- The root placeholder `main.go`/`doc.go`.
- `docs/authDocs/` (a stale copy of goAuth v0.3/v0.4 docs), replaced by links
  pinned to goAuth v0.5.0 in `docs/auth-goauth.md`.
- `auth.TenancySettings.EnforceIsolation`.

### Fixed

- Account-status transitions (email-verification confirm, disable, lock)
  failed with goAuth's "account state transition failed": the provider never
  advanced `account_version`.
- `TENANCY_ENABLED=true` failed at startup on goAuth v0.5.0 (the provider did
  not implement `TenantAwareUserProvider`), and nothing attached the request
  tenant.
- Duplicate registrations surfaced as a generic provider error instead of
  goAuth's `ErrAccountExists`.
- `make` could not find `MAKEFILE` on Linux/macOS; `make run` did not run the
  server.
- `seed-users.ps1` passed `--create-if-missing true` as two arguments, which
  stopped Go flag parsing and dropped the flags after it.
- Migration 000004's header no longer calls it optional (it is always applied
  and inert until enabled).

### Documentation

- New: `docs/getting-started.md` (clone -> init -> run),
  `docs/auth-flows.md` (every endpoint, flag, shape, error code, enumeration
  notes), `docs/multi-tenancy.md` (tenant model, resolver config, uniqueness
  choice, v0.5.0 fixes, adoption, audit-event warning).
- Rewritten: `docs/auth-goauth.md`, `docs/auth-bootstrap.md` (make user, users
  schema, roles), README (Quick Start with `make init`, goAuth v0.5.0),
  `docs/trim-to-what-you-need.md` (init pruning flags, per-feature auth
  deletion), `docs/removing-tenancy.md` (middleware, migrations, provider
  methods).
- Updated: `AGENTS.md`, `docs/environment-variables.md`, `docs/policies.md`,
  `docs/architecture.md`, `docs/enabling-webauthn.md`, `docs/overview.md`,
  `docs/workflows.md`, `docs/performance-testing.md` (Windows-first note,
  `AUTH_TEST_*` guard), `docs/security-env-recommendations.md`,
  `CONTRIBUTING.md`.

### Verification

- `make sqlc-generate` is idempotent (no diff on a second run).
- `go build ./...`, `go vet ./...`, `golangci-lint run` (v1.64.6),
  `go run ./cmd/superapi-verify ./...` pass.
- `go test ./... -race` passes with and without `TENANCY_ENABLED=true`, and with
  `SUPERAPI_TEST_DATABASE_URL` against Postgres 16.
- Migrations 000001–000006 apply, roll back fully, and re-apply.
- `templateinit` default and `--no-all` projects pass build, vet, verify, tests
  and sqlc drift.
- Manual smoke test against Postgres + Redis: tenancy (missing/unknown tenant,
  cross-tenant login and token reuse) and the full lifecycle (register,
  verification gate, verify, TOTP enroll + MFA login, reset).

## v0.8.0 (2026-07-15)

A structural sweep of the data layer, auth, and tenancy, plus an optional
document store and a documentation refresh. The headline change is that the
relational data layer is now sqlc over pgx behind a single thin transaction
boundary, and two subsystems (tenancy, the document store) are now genuinely
optional.

**Behavior changes to note when upgrading:**

- **Tenancy is now off by default** (`TENANCY_ENABLED=false`). Preset policy
  chains no longer default to tenant scoping/keying: authenticated cache reads
  vary by user id instead of tenant id, and a `{tenant_id}` path parameter is
  treated as an ordinary parameter rather than forcing `TenantRequired` +
  `TenantMatchFromPath` onto the route. Set `TENANCY_ENABLED=true` to restore the
  previous tenant-strict behavior; the tenant policies still enforce correctly
  when attached explicitly.
- **The `internal/core/storage` API changed.** The `Store` / `RelationalStore` /
  `DocumentStore` contracts and their operation builders were removed in favor of
  a single `storage.Postgres` type. Downstream code that used the old store
  surface must move to `DB().Queries(ctx)` (repositories) and `DB().WithTx(...)`
  (services).

### Added

- **sqlc-based relational data layer with a thin transaction boundary.**
  `internal/core/storage` now exposes one `Postgres` type. `Queries(ctx)` returns
  sqlc-generated queries bound to the transaction carried in the context (via
  `WithTx`) or to the pool otherwise; `WithTx(ctx, fn)` owns the write
  transaction lifecycle with panic-safe rollback. Repositories obtain
  `*sqlcgen.Queries` per call, so sqlc/pgx types never leak into service or module
  interfaces. `modulekit.Runtime` exposes it to modules via `DB()`.

- **Optional, self-contained document (NoSQL) store** (`internal/storage/document`,
  outside `internal/core`), designed for a painless swap to MongoDB or any
  document backend.
  - `Store` interface (`Collection(ctx, name)` yielding
    `Get`/`Insert`/`Replace`/`Delete`/`Find`) with explicit write intent —
    `Insert` fails on a duplicate id (`ErrAlreadyExists`), `Replace` upserts —
    mapping 1:1 onto Mongo's `InsertOne` / `ReplaceOne(upsert)`.
  - `Find` takes a `Query` with a portable `Fields` exact-match conjunction plus
    an optional backend-specific `Native` value (e.g. a Mongo `bson.M`), so a
    backend exposes its full query power without the interface leaking driver
    types.
  - Transactions are an optional capability (`TxStore`); the free
    `document.WithTx(ctx, store, fn)` helper runs a unit of work atomically when
    the backend supports transactions and directly otherwise, so a standalone
    MongoDB (no multi-document transactions) still works.
  - Ships a dependency-free `InMemoryStore` reference implementation with
    copy-on-write transaction staging, and a compiling example
    (`internal/storage/document/example`) showing the handler → service →
    repository pattern with no `if sql else mongo` branching (not registered as a
    runtime module).
  - Core keeps zero references, so an unused document store is excluded from the
    binary (dead-code elimination) and deleting the folder is a clean removal. It
    shares nothing with the Redis response cache. See `docs/document-store.md`,
    which includes a complete drop-in MongoDB adapter.

- **goAuth v0.4.0 features** (all opt-in, wired through the auth customization
  point and the system module).
  - **Remember-me + session ceiling:** login accepts `remember_me`;
    `AUTH_MAX_SESSION_DURATION` caps absolute session lifetime.
  - **MFA-aware login + confirm endpoint:** login returns an MFA challenge
    (`mfa_required` / `mfa_challenge` / `mfa_type` / `mfa_types`) instead of
    tokens when a second factor is required; complete it at
    `POST /api/v1/system/auth/mfa/confirm` (`ConfirmLoginMFAWithType`).
  - **Graceful logout endpoint (new):** `POST /api/v1/system/auth/logout` revokes
    the session via `LogoutByAccessToken`, accepting an expired-but-authentic
    access token (from the body or the `Authorization: Bearer` header). This
    closes a real gap — there was no logout route before.
  - **Sliding-window auth limiter:** `AUTH_LIMITER_WINDOW_MODE=sliding` selects
    goAuth's internal auth-abuse limiter algorithm.
  - **Ed25519 key rotation:** `AUTH_KEY_ID` + `AUTH_VERIFY_KEYS` populate goAuth's
    `JWT.KeyID` / `JWT.VerifyKeys`, with the "set both or neither" invariant
    enforced at startup.
  - **WebAuthn — scaffolded, disabled by default:** `WebAuthnCredentialProvider`
    implemented on `StoreUserProvider` over a new WebAuthn credential repository;
    auth-protected ceremony endpoints under `/api/v1/system/auth/webauthn/*`; an
    optional migration (`000004_webauthn_credentials`) applied only when enabling.
    goAuth does not require the WebAuthn capability at Build while disabled, and
    the endpoints return a "webauthn disabled" error until turned on. See
    `docs/enabling-webauthn.md`.
  - Introduced a thin `system` auth service so login/refresh/logout/MFA and the
    WebAuthn ceremonies flow through handler → service, matching the enforced
    architecture.

- **Optional multi-tenancy behind a single flag.** New `TENANCY_ENABLED`
  (default `false`) and `TENANCY_ENFORCE_ISOLATION` config
  (`config.TenancyConfig`) with a lint rule (`TENANCY_ENFORCE_ISOLATION` requires
  `TENANCY_ENABLED`). The policy engine gained a tenancy toggle
  (`policy.SetTenancyEnabled` / `policy.TenancyEnabled`), applied once at startup
  before any route registers; its package default is enabled so tests and
  consumers that never configure it keep strict tenant behavior. Tenant policies
  and presets (`TenantRequired`, `TenantMatchFromPath`, `TenantRead`,
  `TenantWrite`) moved into `internal/core/policy/tenant.go`. goAuth
  `MultiTenant.Enabled` / `EnforceIsolation` follow the flag via a new
  `auth.TenancySettings` argument threaded through `ProjectGoAuthConfig` and
  `NewGoAuthEngine`.

### Changed

- Upgraded the goAuth dependency from `v0.3.2` to the published `v0.4.0`,
  required directly from the module proxy (no `replace` directive). This pulls in
  goAuth's WebAuthn transitive dependencies unconditionally
  (`go-webauthn/webauthn`, `go-webauthn/x`, `go-tpm`, `fxamacker/cbor`); they are
  compiled in but inert until WebAuthn is enabled.

- Rewrote the auth user repository (`internal/core/auth/user_repository.go`) on
  sqlc queries (`GetAuthUserByLogin`, `GetAuthUserByID`, `CreateAuthUser`,
  `UpdateAuthUserStatus`, `UpdateAuthUserPasswordHash`), dropping the hand-written
  SQL constants. `NewRelationalUserRepository` now takes `*storage.Postgres`.

- Removed the old execution abstraction: `RelationalStore`, `DocumentStore`,
  `RelationalOperation`, `DocumentOperation`, `RelationalExecutor`, `RowScanner`,
  the `RelationalExec/QueryOne/QueryMany` and `DocumentRun` builders,
  `PostgresRelationalStore`, and `NoopDocumentStore`. `Dependencies` collapses
  `Store` / `RelationalStore` / `DocumentStore` into a single `DB
  *storage.Postgres`; `modulekit.Runtime` replaces
  `Store()`/`RelationalStore()`/`DocumentStore()` with `DB()`. `cmd/perftoken` is
  migrated to the new boundary.

- The system module's login response gained an MFA-challenge shape and a
  `remember_me` request field; `whoami` is now registered once (previously via
  duplicated branches).

- Slimmed the DevX generators to the sqlc data-layer pattern.
  - `modulegen` now scaffolds the sqlc pattern for DB-enabled modules: the
    repository holds `*storage.Postgres` and calls `Queries(ctx)`, the module
    wires it from `runtime.DB()`, and the service owns the `WithTx` write boundary
    (example shown). Tenant scaffolding emits a note that `TenantRequired` needs
    `TENANCY_ENABLED=true`.
  - `authgen` no longer generates a parallel `SQLCUserProvider` or rewrites
    `deps.go` / `goauth_provider.go` — those steps targeted APIs removed in this
    sweep and would have produced non-compiling output. The template already ships
    a working `StoreUserProvider`; authgen now scaffolds the auth data layer
    (migration, schema, queries) and docs only.

### Fixed

- Not-found parity regression in the sqlc swap: `UpdateAuthUserPasswordHash` was
  changed from `:exec` to `:one … RETURNING id` (and regenerated) so a password
  update for a missing/deleted user surfaces `ErrAuthUserNotFound` instead of
  silently reporting success through goAuth's password-reset path.

- Committed sqlc drift: removed the orphaned
  `internal/core/db/sqlcgen/system_settings.sql.go` (no query source, duplicated
  the system module's synced queries, causing a redeclaration build error),
  removed the false "generated by modulesync" header from the hand-authored
  `db/queries/tenants.sql` and `db/schema/tenants.sql`, and hardened modulesync's
  managed-header detection to normalize CRLF. `make sqlc-generate` is now
  idempotent.

### Documentation

- Migrated the prose docs off the removed store abstraction and onto the sqlc
  boundary: `docs/architecture.md`, `docs/module_guide.md`, `docs/modules.md`,
  `docs/workflows.md`, `docs/crud-examples.md`, `docs/auth-goauth.md`,
  `docs/auth-bootstrap.md`, and `docs/overview.md` now describe
  `DB().Queries(ctx)` / `DB().WithTx(...)`, and `AGENTS.md` was rewritten to
  match.

- Added `docs/transactions.md`: a focused guide to the two-method transaction
  model (`Queries(ctx)` in repositories, `WithTx` in services), how context
  binding works, and the "always thread the context" gotcha.

- Added `docs/trim-to-what-you-need.md`: a per-feature "disable via config vs
  delete the code" checklist covering the DevX generators, auth, WebAuthn,
  tenancy, the document store, response cache, rate limiting, Postgres/Redis,
  observability, and example modules — with exact files and wiring edits. Also
  added `docs/removing-tenancy.md`, `docs/enabling-webauthn.md`, and
  `docs/document-store.md`.

- Refreshed `README.md`: corrected the feature and data-layer lists, added a
  "Why SuperAPI / Problems It Solves" problem→solution table and a "Highlights"
  section, a "Trim To What You Need" section, and bumped the release badge and
  baseline to v0.8.0. Mirrored a shortened "Why This Template" section into
  `docs/overview.md`. Documented the hybrid-mode guarantee and the `ModeJWTOnly`
  downgrade caveat in `docs/policies.md`.

### Verification

- `go build ./...`, `go vet ./...`, `go test ./...`, and
  `go run ./cmd/superapi-verify ./...` all pass: clean build, green tests, and
  the architecture gate green.

## v0.7.3 (2026-06-08)

### Added

#### Auth Customization Layer

Added dedicated goAuth customization points under `internal/core/auth` to make authentication behavior easier to modify without changing provider wiring.

- Added `ProjectGoAuthConfig()` as the primary goAuth configuration entrypoint.
- Added built-in goAuth lint integration with startup failure on high-severity findings.
- Added documented customization examples for JWT identity, signing keys, token lifetimes, and security settings.
- Added explicit role and permission registry definitions via `roles.go`.
- Added template guidance for project-specific auth configuration and role customization.

#### Documentation

- Improved auth configuration discoverability with inline customization guidance.
- Added examples for JWT overrides, production hardening, and performance-testing configuration.

## v0.7.1 (2026-04-06)

### Fixed
- System auth demo routes were aligned with goAuth v0.3.0 error semantics.
	- Removed usage of deleted `goauth.ErrRefreshRateLimited`.
	- Added canonical auth error translation for login/refresh endpoints based on goAuth `AuthError` categories.
	- Mapped auth abuse/state/system categories to stable API responses (`429`/`403`/`503`/`500`) while preserving unauthorized defaults.

### Added
- New focused tests for system auth route error translation.
	- Added category-based mapping coverage for `AUTH_ABUSE`, `AUTH_STATE`, `SYSTEM_INTERNAL_ERROR`, and `SYSTEM_UNAVAILABLE`.
	- Added fallback coverage for legacy `goauth.ErrLoginRateLimited` sentinel matching.

### Changed
- goAuth dependency was upgraded to `v0.3.0`.
- Auth docs were migrated to the v0.3.0 model, including:
	- Canonical `AuthError` boundary and code registry documentation.
	- Updated limiter and config field naming (`EnableLoginFailureLimiter`, request/confirm limiter split fields, and creation limiter toggle).
	- Refresh-throttle removal guidance and v0.3.0 migration notes.

## v0.7.0 (2026-04-05)

### Breaking Changes
- Enforced store-first data-layer architecture across runtime wiring and module guidance.
	- Required flow: Service -> Repository -> Store -> Backend
- Removed legacy core DB helper APIs from `internal/core/db`.
	- Removed `db.NewQueries(...)`
	- Removed `db.QueriesFrom(...)`
	- Removed `db.QueriesFromTx(...)`
	- Removed `db.WithTx(...)`
	- Removed `db.WithTxResult(...)`
- `modulekit.Runtime` storage access surface changed.
	- Removed `Runtime.Postgres()` accessor
	- Added `Runtime.Store()`, `Runtime.RelationalStore()`, `Runtime.DocumentStore()`
- goAuth provider constructor and wiring path changed.
	- Replaced `auth.NewSQLCUserProvider(...)` with `auth.NewStoreUserProvider(...)`
	- Auth persistence now goes through repository + store contracts

### Added
- New storage contracts package at `internal/core/storage`.
	- Backend kind contract (`Store.Kind()`)
	- Mandatory transaction contract (`TransactionalStore.WithTx(...)`)
	- Relational/document operation execution contracts
- New relational store implementation over pgx.
	- `storage.PostgresRelationalStore`
- New document contract placeholder implementation.
	- `storage.NoopDocumentStore`
- New auth repository over relational store.
	- `internal/core/auth/user_repository.go`
- New operation helpers for repository-defined execution.
	- `storage.RelationalExec(...)`
	- `storage.RelationalQueryOne(...)`
	- `storage.RelationalQueryMany(...)`
	- `storage.DocumentRun(...)`

### Changed
- App dependency wiring now initializes store surfaces when Postgres is enabled.
	- Added `Dependencies.Store`
	- Added `Dependencies.RelationalStore`
	- Added `Dependencies.DocumentStore`
- Auth engine wiring now uses store-backed provider path.
	- `StoreUserProvider -> UserRepository -> RelationalStore -> Postgres`
- `cmd/perftoken` updated to match new auth/store wiring.
- Core DB package scope narrowed to Postgres connectivity and migrations for storage backends.

### Removed
- Legacy core DB helper files:
	- `internal/core/db/queries.go`
	- `internal/core/db/queries_test.go`
	- `internal/core/db/tx.go`
	- `internal/core/db/tx_test.go`

### Documentation
- Rewrote architecture/docs set for store-first model with beginner-focused detail:
	- `docs/overview.md`
	- `docs/architecture.md`
	- `docs/modules.md`
	- `docs/module_guide.md`
	- `docs/crud-examples.md`
	- `docs/workflows.md`
	- `docs/environment-variables.md`
	- `docs/auth-goauth.md`
- Updated auth bootstrap docs for store-backed provider and repository wiring.
- Updated governance instructions in `AGENTS.md` for enforced data-layer constraints.

## v0.6.0 (2026-04-05)

### Breaking Changes
- Cache config model changed from static tags to structured dynamic tag specs.
	- `cache.CacheReadConfig.Tags` -> `cache.CacheReadConfig.TagSpecs`
	- `cache.CacheInvalidateConfig.Tags` -> `cache.CacheInvalidateConfig.TagSpecs`
- Preset option APIs now accept `cache.CacheTagSpec` values.
	- `policy.WithCache(ttl, ...)`
	- `policy.WithInvalidateTags(...)`
- Cache invalidation metadata was renamed for analyzer/validator consistency.
	- `CacheInvalidateMetadata.TagCount` -> `CacheInvalidateMetadata.TagSpecCount`

### Added
- Dynamic scoped cache invalidation tags (`cache.CacheTagSpec`) with runtime resolution from:
	- path params
	- authenticated tenant/user context
	- literal key/value scope dimensions
- New cache key template preparation and reuse path for lower overhead on hot routes.
- In-process cache tag-version token memoization with configurable TTL.
	- New env var: `CACHE_TAG_VERSION_CACHE_TTL` (default `250ms`)
- New browser/proxy cache directive policy:
	- `policy.CacheControl(policy.CacheControlConfig{...})`
- New middleware instrumentation controls:
	- `HTTP_MIDDLEWARE_TRACING_EXCLUDE_PATHS`
	- `METRICS_EXCLUDE_PATHS`
- New tests and benchmarks across app/httpx/cache/metrics/policy modules.

### Changed
- Cache read path now resolves and validates scoped tag names before key generation.
- Cache invalidation now resolves scoped tags from request/auth context and bumps only resolved scopes after successful `2xx` writes.
- Cache and rate-limit prod defaults are now fail-closed by default, with startup lint rejecting fail-open in prod when enabled.
- Tracing middleware supports exact-path exclusion and improved response-writer capability forwarding.
- Request-timeout middleware now bypasses SSE and websocket upgrade flows.
- Metrics middleware supports excluded paths and improved route-pattern propagation through wrapped writers.
- CORS denied preflight and metrics auth failures now return standardized error envelopes.
- Adapter decode path reduced repeated generic checks for request types without bodies.

### Documentation
- Rewrote cache documentation for dynamic tag specs, bump-miss invalidation, and scoped invalidation strategies.
- Updated policy reference with:
	- `TagSpecs` model
	- `CacheControl` policy usage and validation
	- revised preset and route stack examples
- Updated module and CRUD guides to replace legacy static tag examples with scoped `TagSpecs` examples.
- Expanded environment variable docs for new cache, tracing, metrics, and prod hardening settings.

## v0.5.0
- Public template release baseline
- Module system
- Strict policy engine
- goAuth integration
- Redis-backed cache and rate limiting
- Observability foundations (metrics, tracing, structured logs)
- Scaffolder workflow for module generation

## Changelog Rules
- Every release must update this file.
- Entries must be specific and verifiable.
- Avoid vague notes like "misc fixes".
