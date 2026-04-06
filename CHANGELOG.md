# Changelog

All notable changes to this template are documented in this file.

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
