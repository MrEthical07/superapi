# Trim To What You Need

This template is plug-and-play in both directions: every optional subsystem can
be **turned off with config** (zero code changes) and most can be **deleted
entirely** as a bounded, greppable change. "Use only what you need, delete the
rest."

Two levels for almost everything:

1. **Disable (recommended first step).** Flip a config flag. Nothing runs, but
   the code stays so you can turn it back on. This is always safe and reversible.
2. **Delete.** Remove the code once you are sure you will never use it. Each
   section below lists the exact files and the wiring edits (if any).

After any deletion, run the gates:

```
go build ./...
go test ./...
go run ./cmd/superapi-verify ./...
```

If it builds, tests pass, and verify is green, the removal is complete.

<!-- template:begin init -->
## Prune at init (fresh clones)

On a fresh clone, `make init` can delete whole features for you and leaves a
project that passes the gate (CI checks the default and `--no-all`):

```bash
make init module=github.com/acme/foo name="Foo API" flags="--no-tenancy --no-webauthn --no-perf"
```

| Flag | What it removes |
|---|---|
| `--no-tenancy` | tenant resolver/directory, `tenants` table (migration 000002), `TENANCY_*` config, tenancy docs, `make user tenant=`. Keeps the generic tenant policy primitives and `users.tenant_id` (always `'0'`); remove those manually with [removing-tenancy.md](removing-tenancy.md) |
| `--no-webauthn` | migration 000004, schema/queries/sqlc output, the credential repository and provider methods, ceremony routes, `WEBAUTHN_*` config |
| `--no-document-store` | `internal/storage/document/` and its doc |
| `--no-devx` | `cmd/modulegen`, `cmd/modulesync`, `internal/devx/`, `make module`/`db-sync` (`make sqlc-generate` then runs sqlc directly) |
| `--no-perf` | `performance/`, `cmd/perftoken`, `make perf-token`/`load-*`, the perf runbook |
| `--no-demo` | bundled example code (`internal/storage/document/example/`) |
| `--no-all` | everything above |

`--dry-run` previews; `--keep-init` keeps the tool and its markers so you can
prune more later. The sections below are the manual equivalents.

<!-- template:end init -->
> Tip: `APP_PROFILE=minimal` disables Postgres, Redis, auth, cache, and rate
> limiting in one shot — the fastest way to see how little the template needs to
> run. See docs/environment-variables.md.

---

## Quick reference

| Feature | Disable (config) | Delete (code) | Notes |
|---|---|---|---|
| Auth (goAuth) | `AUTH_ENABLED=false` | see [Auth](#auth-goauth) | engine is nil when off; no auth routes are registered |
| Auth features (register, reset, verification, TOTP) | `AUTH_*_ENABLED=false` (default) | see [Auth features](#individual-auth-features) | routes not registered when off |
| WebAuthn | `WEBAUTHN_ENABLED=false` (default) | see [WebAuthn](#webauthn) | table always migrated, inert until enabled |
| Tenancy | `TENANCY_ENABLED=false` (default) | see [docs/removing-tenancy.md](removing-tenancy.md) | off by default |
| Document store | not wired by default | delete the folder | see [Document store](#optional-document-store) |
| Response cache | `CACHE_ENABLED=false` | see [Cache](#response-cache) | policies degrade to pass-through |
| Rate limiting | `RATELIMIT_ENABLED=false` | see [Rate limiting](#rate-limiting) | |
| Postgres | `POSTGRES_ENABLED=false` | keep unless auth/DB modules removed | auth needs it |
| Redis | `REDIS_ENABLED=false` | keep unless auth/cache/ratelimit removed | those need it |
| Metrics/Tracing | `METRICS_ENABLED=false` / `TRACING_ENABLED=false` | see [Observability](#observability) | woven into middleware |
| DevX generators | n/a (dev-only) | delete `cmd/modulegen`, `cmd/modulesync`, `internal/devx/*` | never in the app binary |
| Perf tooling | n/a (dev-only) | delete `performance/`, `cmd/perftoken` | never in the app binary |
| Example modules | n/a | delete the module folder + registry line | |

---

## DevX generators (modulegen, modulesync)

These are **developer tools**, not part of the running API — they never appear
in the `cmd/api` binary. Once your project structure is stable you can delete
any you will not use.

- **modulegen** (`make module`): scaffolds new modules. Delete `cmd/modulegen/`
  and `internal/devx/modulegen/` if you will hand-write modules.
- **modulesync** (`make sqlc-generate` runs it first): copies module-local
  `db/*.sql` into root `db/` for sqlc. Only useful if you keep SQL inside module
  folders. If you author SQL directly under `db/queries` and `db/schema`, delete
  `cmd/modulesync/` and `internal/devx/modulesync/`, and change the `Makefile`
  `sqlc-generate` target to just `$(SQLC) generate`.
- **createuser** (`make user`): creates accounts through the configured
  goAuth engine. Keep it; it is how you bootstrap the first admin.
- **superapi-verify** (`make verify`): the architecture/policy static checker.
  Keep it — it is cheap insurance — but it is dev-only and deletable
  (`cmd/superapi-verify/`) with no runtime impact.

Deleting a generator has no effect on the app binary; just drop the matching
`Makefile` targets.

---

## Auth (goAuth)

**Disable:** `AUTH_ENABLED=false`. The goAuth engine is not built and the auth
module registers no routes. Everything else runs.

**Delete entirely** (if your API is fully public):

1. Remove the auth wiring block in `internal/core/app/deps.go` (the
   `if cfg.Auth.Enabled { ... }` section that builds the user provider and
   engine, plus the `authClose` handling in `closeDependencies`).
2. Remove `AuthEngine` / `AuthMode` from `Dependencies` and the corresponding
   `modulekit.Runtime` accessors.
3. Delete `internal/core/auth/` (provider, config, roles, repositories).
4. Delete `internal/modules/auth/` and its line in `internal/modules/modules.go`,
   `internal/core/notify/`, `cmd/createuser/`, and the `AuthUsers`/`Notifier`
   dependency fields.
5. Delete the auth migrations (`000003`, `000004`, `000005`, `000006`),
   `db/schema/auth_*.sql`, `db/schema/webauthn_credentials.sql`,
   `db/queries/auth_*.sql`, `db/queries/webauthn_credentials.sql`, and re-run
   `make sqlc-generate`.
6. Remove the `policy.AuthRequired` / RBAC usage from any route, and drop the
   `AUTH_*` env from config `Load`/`Lint`.

Auth pulls in Postgres and Redis; if nothing else uses them you can disable
those too.

---

## Individual auth features

Each optional group is off by default and its routes are not registered while
off. To delete one:

- **Registration:** the `register` route and handler in `internal/modules/auth`
  (`routes.go`, `handler.go`, `service.go`), `AUTH_REGISTRATION_*` config.
- **Password reset:** the two `password/reset/*` routes, handlers and service
  methods, `AUTH_PASSWORD_RESET_ENABLED`. If email verification is gone too,
  delete `internal/core/notify/` and the account repository lookup in
  `internal/modules/auth/repo.go`.
- **Email verification:** the `email/verify/*` routes, handlers, service
  methods and the verification call in `register`, the
  `AUTH_EMAIL_VERIFICATION_*` config.
- **TOTP + backup codes:** the `mfa/totp/*` and `mfa/backup-codes/*` routes,
  handlers and service methods; `mfa_repository.go`, `secret_cipher.go` and the
  TOTP provider methods (return the old stubs); migration 000006's
  `user_totp`/`user_backup_codes` tables and `db/*/auth_mfa.sql`; the
  `AUTH_TOTP_*` config. Keep `users.account_version`: goAuth needs it for
  status transitions.

## WebAuthn

WebAuthn is **off by default** (`WEBAUTHN_ENABLED=false`) and forces no schema.
If you will never use it, delete it cleanly (see docs/enabling-webauthn.md,
"Removing WebAuthn entirely"):

- `db/migrations/000004_webauthn_credentials.*`, `db/schema/webauthn_credentials.sql`,
  `db/queries/webauthn_credentials.sql` (then `make sqlc-generate`).
- `internal/core/auth/webauthn_repository.go`, `provider_webauthn.go` and
  `config_webauthn.go`, plus the `webauthnRepo` field on `StoreUserProvider`.
- The `WithWebAuthnRepository(...)` call in `deps.go` and the
  `applyWebAuthnConfig` call in `internal/core/auth/config.go`.
- The WebAuthn ceremony endpoints (`internal/modules/auth/webauthn.go` and the
  WebAuthn block in `routes.go`) and the `WEBAUTHN_*` config.

Leaving it disabled costs nothing at runtime.

---

## Tenancy

Off by default (`TENANCY_ENABLED=false`) and fully deletable. See the dedicated
guide: **[docs/removing-tenancy.md](removing-tenancy.md)**.

---

## Optional document (NoSQL) store

The `internal/storage/document` package is **not wired into anything** by
default — it is excluded from the `cmd/api` binary until a module imports it.

- **Not using it?** Delete `internal/storage/document/` (and any module wiring
  you added). Nothing in core references it. See docs/document-store.md.
- **Using a different backend?** Keep the package and implement `document.Store`
  for your backend (a MongoDB adapter is documented); delete the bundled
  `example/` folder.

---

## Response cache

**Disable:** `CACHE_ENABLED=false`. The cache manager is nil; cache policies
degrade to pass-through (no caching, requests still served).

**Delete:** remove the `if cfg.Cache.Enabled { ... }` block in `deps.go`, the
`CacheMgr` field and `Runtime.CacheManager()`, the cache policies from routes
(`policy.CacheRead` / `CacheInvalidate` / `CacheControl`), and — if unused
elsewhere — `internal/core/cache/`. Drop the `CACHE_*` config. Note the response
cache is **independent of the optional document store**; removing one does not
affect the other.

---

## Rate limiting

**Disable:** `RATELIMIT_ENABLED=false`. The limiter is nil; rate-limit policies
become no-ops.

**Delete:** remove the `if cfg.RateLimit.Enabled { ... }` block in `deps.go`,
the `Limiter` field and `Runtime.Limiter()`, the `policy.RateLimit*` usage from
routes, and — if unused — `internal/core/ratelimit/`. Drop the `RATELIMIT_*`
config.

---

## Postgres / Redis

These are shared infrastructure, so delete them only after removing everything
that depends on them.

- **Postgres** backs auth and any DB module. Disable with
  `POSTGRES_ENABLED=false`. Delete only if you have removed auth and all
  relational modules: drop the `if cfg.Postgres.Enabled` block in `deps.go`, the
  `Postgres`/`DB` fields, `internal/core/storage`, `internal/core/db`, and the
  `db/` SQL tree.
- **Redis** backs auth (sessions), the response cache, and rate limiting.
  Disable with `REDIS_ENABLED=false`. Delete only after those three are gone.

---

## Observability (metrics, tracing, access logs)

**Disable:** `METRICS_ENABLED=false`, `TRACING_ENABLED=false`, and the access-log
middleware toggle. These are safe to turn off in any environment.

**Delete** is more involved than the toggles above because observability is
woven into the HTTP middleware chain, not just `deps.go`:

- Metrics: `internal/core/metrics/`, the `Metrics` dep + readiness/HTTP
  instrumentation in `deps.go`/`app.go`, and the metrics touch-points in the
  `health` module and `httpx` middleware.
- Tracing: `internal/core/tracing/`, the `Tracing` dep and shutdown, and
  `internal/core/httpx/tracing.go` plus its use in `globalmiddleware.go`.

Prefer disabling over deleting these unless you have a strong reason — they carry
little runtime cost when disabled and are useful in production.

---

## Example / unused modules

Modules are registered in `internal/modules/modules.go`. To remove one:

1. Delete its folder under `internal/modules/<name>/`.
2. Remove its import and its `<name>.New()` line from `modules.go`.
3. If it had DB SQL, remove the synced `db/schema/<name>.sql` and
   `db/queries/<name>.sql`, then `make sqlc-generate`.

The `health` module is a good keep (liveness/readiness); the `auth` module
exposes only the endpoint groups you enable.

---

## What you cannot remove

A small core is always required: the HTTP server and router (`internal/core/httpx`,
`cmd/api`), config (`internal/core/config`), logging (`internal/core/logx`),
error/response helpers, the module/runtime kit, and the policy engine
(`internal/core/policy`) that route registration depends on. Everything else in
the table above is optional.
