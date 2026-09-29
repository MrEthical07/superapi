package modulegen

// The tenant scaffolding: `make module ... flags=--tenant` attaches the tenant
// policy to the generated route and scopes its cache by tenant. This file is
// removed together with the rest of tenancy (make init --no-tenancy), which
// also removes the flag.
func init() {
	RegisterExtension(Extension{
		Flag:         "tenant",
		Help:         "attach tenant policy to generated route",
		Prompt:       "Require tenant scope on the generated route?",
		RequiresAuth: true,
		Imports:      []string{`"github.com/MrEthical07/superapi/internal/tenancy"`},
		Notes: []string{
			"NOTE: TenantRequired() below needs TENANCY_ENABLED=true at runtime to be",
			"meaningful: with tenancy off every principal carries goAuth's default tenant.",
			"Remove the tenant policy (and --tenant) if this module is not tenant-scoped.",
		},
		Policies:  []string{"\t\ttenancy.TenantRequired(),"},
		CachePart: "tenancy.CacheVary()",
	})
}
