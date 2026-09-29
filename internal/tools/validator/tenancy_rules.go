package validator

import (
	"go/ast"

	corepolicy "github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/tenancy"
)

// Static verification of the tenant policies. This file is removed together
// with the rest of tenancy (make init --no-tenancy).
//
// The verifier applies the strict form of the tenancy route rule (a route with
// a {tenant_id} path must carry the tenant policies), the same rule the running
// router applies when TENANCY_ENABLED=true, and it knows tenancy.CacheVary()
// is an identity-bearing cache key part.
func init() {
	RegisterExtension(Extension{
		Policies: map[string]PolicyParser{
			"TenantRequired": func(*ast.CallExpr) corepolicy.Metadata {
				return corepolicy.Metadata{Type: tenancy.PolicyTypeTenantRequired, Name: "TenantRequired", Stage: corepolicy.StageIsolation}
			},
			"TenantMatchFromPath": func(call *ast.CallExpr) corepolicy.Metadata {
				meta := corepolicy.Metadata{Type: tenancy.PolicyTypeTenantMatchFromPath, Name: "TenantMatchFromPath", Stage: corepolicy.StageIsolation}
				if len(call.Args) > 0 {
					if param, err := extractStringLiteral(call.Args[0]); err == nil {
						meta.Data = map[string]string{tenancy.DataPathParam: param}
					}
				}
				return meta
			},
		},
		IdentityParts: []string{"CacheVary", "CacheTag"},
		Rules:         []corepolicy.RouteRule{tenancy.RouteRule(true)},
		Hints: map[string]string{
			"tenantmatchfrompath requires tenantrequired": "add tenancy.TenantRequired() before tenancy.TenantMatchFromPath(...). See docs/policies.md",
			"requires tenantrequired":                     "route path includes {tenant_id}; add tenancy.TenantRequired() and tenancy.TenantMatchFromPath(\"tenant_id\"). See docs/policies.md",
			"is required when rbac or tenant policies":    "add policy.AuthRequired(...) before RBAC/tenant policies. See docs/policies.md",
		},
	})
}
