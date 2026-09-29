package tenancy

import (
	"fmt"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/policy"
)

// tenantIDParam is the path parameter that marks a tenant-scoped route.
const tenantIDParam = "tenant_id"

// RouteRule returns the route rule the router (and the static verifier) applies
// for tenancy.
//
// Always enforced:
//   - TenantRequired needs AuthRequired
//   - TenantMatchFromPath needs TenantRequired before it
//
// With strictPaths (tenancy on) a route whose path contains {tenant_id} must
// also carry TenantRequired and TenantMatchFromPath("tenant_id"). With tenancy
// off {tenant_id} is an ordinary parameter and forces nothing.
func RouteRule(strictPaths bool) policy.RouteRule {
	return func(_, pattern string, metas []policy.Metadata) error {
		requiredAt, matchAt := -1, -1
		var matches []policy.Metadata
		hasAuth := false
		for i, m := range metas {
			switch m.Type {
			case policy.PolicyTypeAuthRequired:
				hasAuth = true
			case PolicyTypeTenantRequired:
				if requiredAt == -1 {
					requiredAt = i
				}
			case PolicyTypeTenantMatchFromPath:
				matches = append(matches, m)
				if matchAt == -1 {
					matchAt = i
				}
			}
		}

		if requiredAt >= 0 && !hasAuth {
			return fmt.Errorf("%s is required when RBAC or tenant policies are configured", policy.PolicyTypeAuthRequired)
		}
		if len(matches) > 0 && requiredAt == -1 {
			return fmt.Errorf("%s requires %s", PolicyTypeTenantMatchFromPath, PolicyTypeTenantRequired)
		}
		if requiredAt >= 0 && matchAt >= 0 && matchAt < requiredAt {
			return fmt.Errorf("policy %s must appear after %s", PolicyTypeTenantMatchFromPath, PolicyTypeTenantRequired)
		}

		if strictPaths && patternContainsTenantID(pattern) {
			if requiredAt == -1 {
				return fmt.Errorf("route %s requires %s", pattern, PolicyTypeTenantRequired)
			}
			if len(matches) == 0 {
				return fmt.Errorf("route %s requires %s", pattern, PolicyTypeTenantMatchFromPath)
			}
			for _, m := range matches {
				if strings.ToLower(strings.TrimSpace(m.Data[DataPathParam])) != tenantIDParam {
					return fmt.Errorf("%s for route %s must use path param %q", PolicyTypeTenantMatchFromPath, pattern, tenantIDParam)
				}
			}
		}
		return nil
	}
}

func patternContainsTenantID(pattern string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(pattern)), "{"+tenantIDParam+"}")
}
