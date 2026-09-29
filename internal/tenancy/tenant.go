package tenancy

import (
	"context"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// AttrTenantID is the principal attribute (auth.AuthContext.Attribute) that
// carries the tenant id. It is also the JSON key under which the whoami
// response reports it.
const AttrTenantID = "tenant_id"

// PrincipalTenant returns the tenant of an authenticated principal, or "".
func PrincipalTenant(principal auth.AuthContext) string {
	return strings.TrimSpace(principal.Attribute(AttrTenantID))
}

// TenantIDFromContext extracts the normalized tenant id from the auth context.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return "", false
	}
	tenantID := PrincipalTenant(principal)
	if tenantID == "" {
		return "", false
	}
	return tenantID, true
}

// RequireTenant returns forbidden error when request has no tenant scope.
func RequireTenant(ctx context.Context) error {
	if _, ok := TenantIDFromContext(ctx); ok {
		return nil
	}
	return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required")
}

// IsSameTenant compares principal tenant and resource tenant identifiers.
func IsSameTenant(principalTenantID, resourceTenantID string) bool {
	return strings.TrimSpace(principalTenantID) != "" && strings.TrimSpace(principalTenantID) == strings.TrimSpace(resourceTenantID)
}
