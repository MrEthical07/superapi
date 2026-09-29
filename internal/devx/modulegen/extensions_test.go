package modulegen

import (
	"strings"
	"testing"
)

// A registered extension shows up in the generated route: its policy after
// AuthRequired, its import and notes, and its cache part replacing the default
// per-user scoping. Nothing about it appears when the option is off.
func TestExtensionScaffolding(t *testing.T) {
	saved := extensions
	t.Cleanup(func() { extensions = saved })
	extensions = nil
	RegisterExtension(Extension{
		Flag:      "org",
		Help:      "scope to an organization",
		Prompt:    "Require an organization?",
		Imports:   []string{`"example.com/x/orgs"`},
		Notes:     []string{"NOTE: OrgRequired needs the orgs feature."},
		Policies:  []string{"\t\torgs.OrgRequired(),"},
		CachePart: "orgs.CacheVary()",
	})

	spec, err := NormalizeName("projects")
	if err != nil {
		t.Fatal(err)
	}
	render := func(extra map[string]bool) string {
		return renderRoutesFile(TemplateConfig{Spec: spec, Options: TemplateOptions{UseAuth: true, UseCache: true, Extra: extra}})
	}

	on := render(map[string]bool{"org": true})
	for _, want := range []string{
		`"example.com/x/orgs"`,
		"// NOTE: OrgRequired needs the orgs feature.",
		"policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),\n\t\torgs.OrgRequired(),",
		"VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{orgs.CacheVary()}}",
		"Parts: []cache.KeyPart{orgs.CacheVary()}}}",
	} {
		if !strings.Contains(on, want) {
			t.Errorf("generated route is missing %q:\n%s", want, on)
		}
	}
	if strings.Contains(on, "UserID: true") {
		t.Errorf("the extension's cache part replaces the per-user default:\n%s", on)
	}

	off := render(nil)
	if strings.Contains(off, "orgs") || !strings.Contains(off, "UserID: true") {
		t.Errorf("without the option the route must be the plain one:\n%s", off)
	}

	if got := Extensions(); len(got) != 1 || got[0].Flag != "org" {
		t.Fatalf("Extensions() = %+v", got)
	}
}
