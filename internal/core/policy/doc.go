// Package policy provides route-level middleware for auth, RBAC, tenant checks, rate limiting, and cache control.
//
// Policies are registered in deterministic order and validated at startup to prevent unsafe route wiring.
package policy
