package tenancy

import (
	"errors"
	"net/http"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/policy"
)

var errTokenTenantMismatch = errors.New("token tenant does not match the request tenant")

// AuthExtension is tenancy's part of AuthRequired.
//
// The principal always carries the tenant id goAuth stamped into the token
// (AttrTenantID), including goAuth's default tenant "0" when tenancy is off.
//
// With tenancy on it also binds the token to the tenant: a token minted for
// another tenant must not authenticate here, whatever the validation mode.
// jwt_only and hybrid routes never load the tenant-keyed session, so this is
// the only check that ties token to tenant. The rejection is the same 401 as
// any invalid token.
func AuthExtension(enabled bool) *policy.AuthExtension {
	ext := &policy.AuthExtension{
		Attribute: func(result *goauth.AuthResult) (string, string) {
			return AttrTenantID, result.TenantID
		},
	}
	if enabled {
		ext.Check = checkTokenTenant
	}
	return ext
}

func checkTokenTenant(r *http.Request, result *goauth.AuthResult) error {
	if requestTenant, ok := RequestTenantFromContext(r.Context()); ok && !IsSameTenant(result.TenantID, requestTenant) {
		return errTokenTenantMismatch
	}
	return nil
}
