// Package auth exposes the goAuth authentication lifecycle over HTTP under
// /api/v1/auth.
//
// Layout follows the enforced module architecture:
//   - dto.go      transport contracts and request validation
//   - handler.go  HTTP handlers (transport only)
//   - service.go  the only code that calls the goAuth engine
//   - repo.go     delivery-address lookup over the auth user repository
//   - routes.go   route + policy registration, gated by AUTH_* feature flags
//
// Endpoint groups that are disabled by config are not registered (404).
// See docs/auth-flows.md for every endpoint, flag, and error code.
package auth
