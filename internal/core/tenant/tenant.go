package tenant

import (
	"context"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// TenantIDFromContext extracts normalized tenant id from auth context.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return "", false
	}
	tenantID := strings.TrimSpace(principal.TenantID)
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
