# Changelog

All notable changes to this template are documented in this file.

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
