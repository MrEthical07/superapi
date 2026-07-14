# Removing Tenancy

Tenancy in this template is optional and gated behind a single config flag. This
page covers the two levels of "off": disabling it at runtime, and deleting it
from the codebase entirely.

## 1. Disable at runtime (no code changes)

Leave `TENANCY_ENABLED` unset or set it to `false` (this is the default):

```
TENANCY_ENABLED=false
```

With tenancy disabled:

- Preset policy chains do not default to tenant scoping/keying. Authenticated
  cache reads vary by user id instead of tenant id, which satisfies the
  "authenticated cache routes must vary by user or tenant" safety rule.
- The route validator treats a `{tenant_id}` path parameter as an ordinary
  parameter; it does not force `TenantRequired` / `TenantMatchFromPath` onto the
  route.
- goAuth's `MultiTenant.Enabled` is set to `false`.

The tenant policies (`policy.TenantRequired()`, `policy.TenantMatchFromPath(...)`)
remain available and enforce correctly if you attach them explicitly. The
dependency rule "`TenantMatchFromPath` requires `TenantRequired`" holds
regardless of the flag.

`TENANCY_ENFORCE_ISOLATION=true` requires `TENANCY_ENABLED=true`; the config lint
rejects the combination otherwise.

## 2. Delete tenancy from the codebase

If your project will never be multi-tenant, tenancy is a bounded, greppable
deletion. Remove, in this order:

1. `internal/core/policy/tenant.go` — the `TenantRequired` / `TenantMatchFromPath`
   policies, the `TenantRead` / `TenantWrite` presets, and the package tenancy
   flag (`SetTenancyEnabled` / `TenancyEnabled`).
2. The tenancy plumbing in `internal/core/policy/options.go`
   (`tenantMatchParam`, the tenant branch in `defaultPresetConfig`) and
   `internal/core/policy/validator_rules.go` (`patternContainsTenantID` and its
   use in `validateTenantRules`), plus the tenant `PolicyType*` constants in
   `internal/core/policy/metadata.go` if unused afterward.
3. `internal/core/tenant/` — the tenant primitives package.
4. The `Tenancy` config block in `internal/core/config/config.go`
   (`TenancyConfig`, the `TENANCY_*` env loads, and the tenancy lint rule).
5. The `TenancySettings` wiring in `internal/core/auth/config.go` and
   `internal/core/auth/goauth_provider.go`, and the call site in
   `internal/core/app/deps.go` (the `policy.SetTenancyEnabled(...)` call and the
   `auth.TenancySettings{...}` argument).
6. The tenant fields in the cache and rate-limit option structs
   (`cache.CacheVaryBy.TenantID`, tag `TenantID`, `ratelimit.ScopeTenant`,
   `KeyByTenant`, and the tenant branch of `KeyByUserOrTenantOrTokenHash`) if you
   want them gone as well — these degrade gracefully and are safe to leave.

After deletion, run the gates:

```
go build ./...
go test ./...
go run ./cmd/superapi-verify ./...
```

## Related

- docs/policies.md — tenant policy reference and the optionality note
- docs/environment-variables.md — `TENANCY_ENABLED`, `TENANCY_ENFORCE_ISOLATION`
