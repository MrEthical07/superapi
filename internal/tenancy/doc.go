// Package tenancy is the optional multi-tenancy feature.
//
// Every piece of tenancy behavior lives in this directory: the resolver
// middleware and tenant directory, its settings (TENANCY_*), the tenant
// policies and presets, the goAuth TenantAwareUserProvider wrapper, the
// token-to-tenant binding check, the cache and rate-limit key parts, the
// request-tenant context helpers and the cmd/createuser flags. Core never
// imports it: the project lists it in internal/features, and it plugs into core
// through app.Hooks (see docs/architecture.md, "Optional features"). Removing
// tenancy is deleting this directory and the files named *tenancy* (make init
// --no-tenancy does it); see docs/removing-tenancy.md.
//
// Tenancy is compiled in but off by default: with TENANCY_ENABLED=false the
// feature only contributes the principal's tenant id (goAuth's default tenant
// "0"), so nothing else changes. See docs/multi-tenancy.md.
package tenancy
