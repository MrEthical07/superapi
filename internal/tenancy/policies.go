package tenancy

import (
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/params"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// Policy types of the tenant policies, as the route validator and the static
// verifier see them.
const (
	// PolicyTypeTenantRequired marks tenant scope enforcement policy.
	PolicyTypeTenantRequired policy.PolicyType = "tenant_required"
	// PolicyTypeTenantMatchFromPath marks path-tenant isolation policy.
	PolicyTypeTenantMatchFromPath policy.PolicyType = "tenant_match_from_path"
)

// DataPathParam is the Metadata.Data key holding TenantMatchFromPath's
// parameter name.
const DataPathParam = "tenant_path_param"

// TenantRequired ensures authenticated requests carry tenant scope.
//
// Behavior:
//   - Returns 401 when authentication context is absent
//   - Returns 403 when tenant scope is missing
//   - Returns 404 when a request tenant was resolved by the tenant middleware
//     (TENANCY_ENABLED=true) and differs from the principal's tenant
//
// Notes:
// - Required for tenant-isolated routes
// - Place after AuthRequired
func TenantRequired() policy.Policy {
	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}
			if err := RequireTenant(r.Context()); err != nil {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), rid)
				return
			}
			if requestTenant, ok := RequestTenantFromContext(r.Context()); ok && !IsSameTenant(PrincipalTenant(principal), requestTenant) {
				response.Error(w, apperr.New(apperr.CodeNotFound, http.StatusNotFound, "not found"), rid)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	return policy.Annotate(p, policy.Metadata{
		Type:  PolicyTypeTenantRequired,
		Name:  "TenantRequired",
		Stage: policy.StageIsolation,
	})
}

// TenantMatchFromPath enforces tenant isolation using a route path parameter.
//
// Behavior:
// - Returns 400 when route tenant parameter is missing
// - Returns 401 when auth context is missing
// - Returns 404 when principal tenant and route tenant mismatch
//
// Usage:
//
//	r.Handle(http.MethodGet, "/api/v1/tenants/{tenant_id}/projects", handler,
//	    policy.AuthRequired(engine, mode),
//	    tenancy.TenantRequired(),
//	    tenancy.TenantMatchFromPath("tenant_id"),
//	)
func TenantMatchFromPath(paramName string) policy.Policy {
	paramName = strings.TrimSpace(paramName)
	if paramName == "" {
		panic("invalid route config: " + string(PolicyTypeTenantMatchFromPath) + " requires a non-empty path parameter name")
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}

			resourceTenant := strings.TrimSpace(params.URLParam(r, paramName))
			if resourceTenant == "" {
				response.Error(w, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, paramName+" is required"), rid)
				return
			}
			if PrincipalTenant(principal) == "" {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), rid)
				return
			}
			if !IsSameTenant(PrincipalTenant(principal), resourceTenant) {
				response.Error(w, apperr.New(apperr.CodeNotFound, http.StatusNotFound, "not found"), rid)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	return policy.Annotate(p, policy.Metadata{
		Type:  PolicyTypeTenantMatchFromPath,
		Name:  "TenantMatchFromPath",
		Stage: policy.StageIsolation,
		Data:  map[string]string{DataPathParam: paramName},
	})
}
