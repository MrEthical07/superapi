# Environment Variables

Source of truth:

- internal/core/config/config.go
- internal/core/config/profile.go

This guide lists defaults, behavior, and important lint constraints.

## 1. How Configuration Is Resolved

Resolution order for each variable:

1. explicit environment variable
2. profile default (from APP_PROFILE)
3. hard-coded fallback in config.Load()

Startup always runs config.Lint(). Invalid values and invalid feature combinations fail fast.

## 2. APP_PROFILE Presets

APP_PROFILE values:

- minimal
- dev
- prod

Preset effects (high-level):

- minimal: disables auth/cache/rate-limit/postgres/redis
- dev: enables local full stack defaults and jwt_only auth mode
- prod: enables strict auth defaults and fail-closed cache/rate-limit behavior

Explicit env vars override profile values.

## 3. Core App Variables

| Env var | Default | Notes |
|---|---|---|
| APP_PROFILE | empty | optional profile preset: minimal, dev, prod |
| APP_ENV | dev | prod/production changes some defaults |
| APP_SERVICE_NAME | api-template | service identity for logs/tracing |

## 4. HTTP Server Transport

| Env var | Default | Notes |
|---|---|---|
| HTTP_ADDR | :8080 | listen address |
| HTTP_READ_HEADER_TIMEOUT | 5s | must be > 0 |
| HTTP_READ_TIMEOUT | 15s | must be > 0 |
| HTTP_WRITE_TIMEOUT | 15s | must be > 0 |
| HTTP_IDLE_TIMEOUT | 60s | must be > 0 |
| HTTP_SHUTDOWN_TIMEOUT | 10s | must be > 0 |
| HTTP_MAX_HEADER_BYTES | 1048576 | must be >= 4096 |

## 5. Global Middleware Variables

### 5.1 Core middleware toggles

| Env var | Default | Notes |
|---|---|---|
| HTTP_MIDDLEWARE_REQUEST_ID_ENABLED | true | request-id middleware |
| HTTP_MIDDLEWARE_RECOVERER_ENABLED | true | panic recover middleware |
| HTTP_MIDDLEWARE_MAX_BODY_BYTES | 1048576 | must be >= 0 |
| HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED | true in prod, else false | security headers toggle |
| HTTP_MIDDLEWARE_REQUEST_TIMEOUT | 0 | disabled when 0; if set must be >= 0 and <= HTTP_WRITE_TIMEOUT |
| HTTP_MIDDLEWARE_TRACING_EXCLUDE_PATHS | /healthz,/readyz,/metrics | comma-separated path list |

### 5.2 Access log middleware

| Env var | Default | Notes |
|---|---|---|
| HTTP_MIDDLEWARE_ACCESS_LOG_ENABLED | true | enables structured access log |
| HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE | 0.05 | must be in [0,1] |
| HTTP_MIDDLEWARE_ACCESS_LOG_EXCLUDE_PATHS | /healthz,/readyz,/metrics | each path must start with / |
| HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD | 2s | must be >= 0 |
| HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_USER_AGENT | false | include user-agent field |
| HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_REMOTE_IP | false | include resolved client IP |

### 5.3 Client IP middleware

| Env var | Default | Notes |
|---|---|---|
| HTTP_TRUSTED_PROXIES | empty | optional CSV of trusted CIDR/IP values |

### 5.4 CORS middleware

| Env var | Default | Notes |
|---|---|---|
| HTTP_MIDDLEWARE_CORS_ENABLED | false | enables CORS middleware |
| HTTP_MIDDLEWARE_CORS_ALLOW_ORIGINS | empty | CSV allow list |
| HTTP_MIDDLEWARE_CORS_DENY_ORIGINS | empty | CSV deny list |
| HTTP_MIDDLEWARE_CORS_ALLOW_METHODS | empty | CSV method list |
| HTTP_MIDDLEWARE_CORS_ALLOW_HEADERS | empty | CSV header list |
| HTTP_MIDDLEWARE_CORS_EXPOSE_HEADERS | empty | CSV expose list |
| HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS | false | cannot be true with wildcard allow origins |
| HTTP_MIDDLEWARE_CORS_MAX_AGE | 0 | must be >= 0 |
| HTTP_MIDDLEWARE_CORS_ALLOW_PRIVATE_NETWORK | false | private network preflight behavior |

## 6. Logging Variables

| Env var | Default | Notes |
|---|---|---|
| LOG_LEVEL | info | debug, info, warn, error, fatal |
| LOG_FORMAT | json | json or text |

## 7. Auth Variables

| Env var | Default | Notes |
|---|---|---|
| AUTH_ENABLED | false | enables goAuth integration |
| AUTH_MODE | hybrid | jwt_only, hybrid, strict |
| AUTH_MAX_SESSION_DURATION | unset | absolute session ceiling (also caps remember-me). Must be >= 1m when set; unset = goAuth per-mode default |
| AUTH_LIMITER_WINDOW_MODE | fixed | goAuth's internal auth-abuse limiter algorithm: `fixed` or `sliding` |
| AUTH_KEY_ID | unset | active JWT signing key id (kid). Required when AUTH_VERIFY_KEYS is set |
| AUTH_VERIFY_KEYS | unset | comma-separated `kid=<ed25519-pem>` entries for zero-downtime key rotation. PEM may use literal newlines or `\n` |

Lint dependency rules:

- AUTH_ENABLED=true requires REDIS_ENABLED=true
- AUTH_ENABLED=true requires POSTGRES_ENABLED=true

Key rotation invariant (enforced by goAuth at Build and by SuperAPI at startup):

- When AUTH_VERIFY_KEYS is set, AUTH_KEY_ID must be set and must name one of the
  entries in the map. Set both or neither.

## 7b. WebAuthn Variables

WebAuthn is scaffolded but disabled by default. When `WEBAUTHN_ENABLED=false`
the ceremony endpoints return a "webauthn disabled" error, goAuth does not
require the WebAuthn capability at Build, and no schema is needed. Enabling is a
config + optional-migration step — see docs/enabling-webauthn.md.

| Env var | Default | Notes |
|---|---|---|
| WEBAUTHN_ENABLED | false | turns the WebAuthn surface on |
| WEBAUTHN_RP_ID | unset | Relying Party ID (effective domain, no scheme/port). Required when enabled |
| WEBAUTHN_RP_DISPLAY_NAME | unset | human-readable Relying Party name. Required when enabled |
| WEBAUTHN_RP_ORIGINS | unset | comma-separated exact origins permitted to complete ceremonies. Required when enabled |
| WEBAUTHN_ATTESTATION_PREFERENCE | none | none, indirect, direct, or enterprise |
| WEBAUTHN_USER_VERIFICATION | preferred | preferred, required, or discouraged |
| WEBAUTHN_CEREMONY_TTL | 2m | how long a begun ceremony stays completable |
| WEBAUTHN_REQUIRE_FOR_LOGIN | false | gate login behind an assertion for users with a credential |
| WEBAUTHN_REJECT_CLONED_AUTHENTICATORS | true | fail assertions whose signature counter regressed |

## 7a. Tenancy Variables

| Env var | Default | Notes |
|---|---|---|
| TENANCY_ENABLED | false | enables multi-tenant policy, cache, and rate-limit behavior |
| TENANCY_ENFORCE_ISOLATION | false | requests goAuth strict tenant isolation; only meaningful when TENANCY_ENABLED=true |

Behavior:

- With TENANCY_ENABLED=false (default), tenancy is inert. Preset policies do not
  default to tenant scoping/keying (authenticated cache reads vary by user id
  instead of tenant id), and a `{tenant_id}` path parameter is treated as an
  ordinary parameter rather than forcing tenant policies onto the route.
- With TENANCY_ENABLED=true, tenant scoping/keying defaults return and
  `{tenant_id}` routes must carry `TenantRequired` + `TenantMatchFromPath`. The
  flag is also propagated to goAuth via `MultiTenant.Enabled`.

Lint dependency rules:

- TENANCY_ENFORCE_ISOLATION=true requires TENANCY_ENABLED=true

See docs/policies.md and docs/removing-tenancy.md.

## 8. Rate-Limit Variables

| Env var | Default | Notes |
|---|---|---|
| RATELIMIT_ENABLED | false | enables redis-backed rate-limiter |
| RATELIMIT_FAIL_OPEN | true non-prod, false prod | prod lint rejects fail-open when enabled |
| RATELIMIT_DEFAULT_LIMIT | 10 | must be > 0 |
| RATELIMIT_DEFAULT_WINDOW | 1m | must be > 0 |

Lint dependency rule:

- RATELIMIT_ENABLED=true requires REDIS_ENABLED=true

## 9. Cache Variables

| Env var | Default | Notes |
|---|---|---|
| CACHE_ENABLED | false | enables response cache manager |
| CACHE_FAIL_OPEN | true non-prod, false prod | prod lint rejects fail-open when enabled |
| CACHE_DEFAULT_MAX_BYTES | 262144 | must be > 0 |
| CACHE_TAG_VERSION_CACHE_TTL | 250ms | must be >= 0 |

Lint dependency rule:

- CACHE_ENABLED=true requires REDIS_ENABLED=true

## 10. Postgres Variables

| Env var | Default | Notes |
|---|---|---|
| POSTGRES_ENABLED | false | enables Postgres dependency wiring |
| POSTGRES_URL | empty | required when enabled |
| POSTGRES_MAX_CONNS | 10 | must be > 0 |
| POSTGRES_MIN_CONNS | 0 | must be >= 0 and <= max |
| POSTGRES_CONN_MAX_LIFETIME | 30m | must be >= 0 |
| POSTGRES_CONN_MAX_IDLE_TIME | 5m | must be >= 0 |
| POSTGRES_STARTUP_PING_TIMEOUT | 3s | must be > 0 |
| POSTGRES_HEALTH_CHECK_TIMEOUT | 1s | must be > 0 |

Runtime note:

- when Postgres is enabled, relational store is initialized and exposed through dependencies/runtime

## 11. Redis Variables

| Env var | Default | Notes |
|---|---|---|
| REDIS_ENABLED | false | enables Redis dependency wiring |
| REDIS_ADDR | 127.0.0.1:6379 | required when enabled |
| REDIS_PASSWORD | empty | optional |
| REDIS_DB | 0 | must be >= 0 |
| REDIS_DIAL_TIMEOUT | 2s | must be > 0 |
| REDIS_READ_TIMEOUT | 2s | must be > 0 |
| REDIS_WRITE_TIMEOUT | 2s | must be > 0 |
| REDIS_POOL_SIZE | 10 | must be > 0 |
| REDIS_MIN_IDLE_CONNS | 0 | must be >= 0 and <= pool size |
| REDIS_STARTUP_PING_TIMEOUT | 3s | must be > 0 |
| REDIS_HEALTH_CHECK_TIMEOUT | 1s | must be > 0 |

## 12. Metrics Variables

| Env var | Default | Notes |
|---|---|---|
| METRICS_ENABLED | true | enables metrics endpoint |
| METRICS_PATH | /metrics | must start with / |
| METRICS_AUTH_TOKEN | empty | required in prod when metrics enabled |
| METRICS_EXCLUDE_PATHS | /healthz,/readyz | CSV path list |

## 13. Tracing Variables

| Env var | Default | Notes |
|---|---|---|
| TRACING_ENABLED | false | enables tracing service |
| TRACING_SERVICE_NAME | empty | falls back to APP_SERVICE_NAME |
| TRACING_EXPORTER | otlpgrpc | currently supported exporter |
| TRACING_OTLP_ENDPOINT | localhost:4317 | required when tracing enabled |
| TRACING_SAMPLER | traceidratio | always_on, always_off, traceidratio |
| TRACING_SAMPLE_RATIO | 0.05 | must be in [0,1] |
| TRACING_INSECURE | true non-prod, false prod | transport security toggle |

## 14. Production Constraints To Remember

When APP_ENV is prod or production:

- rate-limit fail-open is rejected when rate-limit enabled
- cache fail-open is rejected when cache enabled
- metrics auth token is required when metrics enabled
- tracing insecure default changes to false

## 15. Practical Validation Tips

If startup fails due to config:

1. read exact lint error from startup logs
2. verify value format (bool/int/duration/float)
3. verify dependency combinations (auth/cache/rate-limit)
4. verify prod-only constraints

## 16. Related Docs

- [docs/workflows.md](workflows.md)
- [docs/architecture.md](architecture.md)
- [docs/auth-goauth.md](auth-goauth.md)
