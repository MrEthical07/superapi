package auth

import (
	"context"
	"strings"

	goauth "github.com/MrEthical07/goAuth"
)

// DefaultTenantID is goAuth's default tenant. goAuth uses it whenever no tenant
// is attached to the request context, and migration 000005 backfills existing
// users into it.
const DefaultTenantID = "0"

type requestTenantKey struct{}

// WithRequestTenant attaches the resolved request tenant to ctx.
//
// It sets the tenant in two places:
//   - goauth.WithTenantID, which scopes every goAuth user lookup, session key
//     and reset/verification record to the tenant (goAuth never reads headers).
//   - a SuperAPI-owned key, readable via RequestTenantFromContext, so policies
//     and the user provider can see the same tenant goAuth sees.
//
// Only the tenant middleware (internal/core/tenant) and trusted tooling such as
// cmd/createuser should call this. Handlers must never derive a tenant from
// untrusted input themselves.
func WithRequestTenant(ctx context.Context, tenantID string) context.Context {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ctx
	}
	ctx = goauth.WithTenantID(ctx, tenantID)
	return context.WithValue(ctx, requestTenantKey{}, tenantID)
}

// RequestTenantFromContext returns the tenant resolved for this request by the
// tenant middleware. It reports false when no tenant was attached (for example
// when tenancy is disabled).
func RequestTenantFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	tenantID, _ := ctx.Value(requestTenantKey{}).(string)
	if tenantID == "" {
		return "", false
	}
	return tenantID, true
}
