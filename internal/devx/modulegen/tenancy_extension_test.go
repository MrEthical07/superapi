package modulegen

import (
	"strings"
	"testing"
)

// The tenant option scaffolds tenancy.TenantRequired and scopes the cache by
// tenant through the tenancy package's key part.
func TestTenantExtensionScaffolding(t *testing.T) {
	spec, err := NormalizeName("projects")
	if err != nil {
		t.Fatal(err)
	}
	route := renderRoutesFile(TemplateConfig{Spec: spec, Options: TemplateOptions{
		UseAuth: true, UseCache: true, Extra: map[string]bool{"tenant": true},
	}})
	for _, want := range []string{
		`"github.com/MrEthical07/superapi/internal/tenancy"`,
		"tenancy.TenantRequired(),",
		"VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{tenancy.CacheVary()}}",
		"TENANCY_ENABLED=true",
	} {
		if !strings.Contains(route, want) {
			t.Errorf("tenant route is missing %q:\n%s", want, route)
		}
	}
	var found bool
	for _, ext := range Extensions() {
		if ext.Flag == "tenant" {
			found = ext.RequiresAuth
		}
	}
	if !found {
		t.Error("the tenant extension must be registered and require the auth policy")
	}
}
