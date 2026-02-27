package tenant

import (
	"context"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

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

func RequireTenant(ctx context.Context) error {
	if _, ok := TenantIDFromContext(ctx); ok {
		return nil
	}
	return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required")
}

func IsSameTenant(principalTenantID, resourceTenantID string) bool {
	return strings.TrimSpace(principalTenantID) != "" && strings.TrimSpace(principalTenantID) == strings.TrimSpace(resourceTenantID)
}
