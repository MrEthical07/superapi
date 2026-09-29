package validator

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corepolicy "github.com/MrEthical07/superapi/internal/core/policy"
)

type ruleError string

func (e ruleError) Error() string { return string(e) }

const errForbidden = ruleError("route is forbidden by rule")

// A feature registers its policies, identity-bearing cache parts and rules; the
// verifier then treats them like built-in ones.
func TestExtensionPoliciesPartsAndRules(t *testing.T) {
	saved := extensions
	t.Cleanup(func() { extensions = saved })
	extensions = nil
	RegisterExtension(Extension{
		Policies: map[string]PolicyParser{
			"OrgRequired": func(*ast.CallExpr) corepolicy.Metadata {
				return corepolicy.Metadata{Type: corepolicy.PolicyTypeCustom, Stage: corepolicy.StageIsolation}
			},
		},
		IdentityParts: []string{"OrgPart"},
		Rules: []corepolicy.RouteRule{func(_, pattern string, _ []corepolicy.Metadata) error {
			if strings.Contains(pattern, "/forbidden") {
				return errForbidden
			}
			return nil
		}},
		Hints: map[string]string{"org rule": "see docs"},
	})

	dir := t.TempDir()
	src := `package m

func register(r router) {
	r.Handle(http.MethodGet, "/ok", h, policy.AuthRequired(e, m), org.OrgRequired(),
		policy.CacheRead(c, cache.CacheReadConfig{TTL: 1, VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{org.OrgPart()}}}))
	r.Handle(http.MethodGet, "/unsafe", h, policy.AuthRequired(e, m),
		policy.CacheRead(c, cache.CacheReadConfig{TTL: 1, VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{other.NotIdentity()}}}))
	r.Handle(http.MethodGet, "/forbidden", h, policy.AuthRequired(e, m))
	r.Handle(http.MethodGet, "/late", h, policy.RateLimit(l, rule), org.OrgRequired())
}
`
	file := filepath.Join(dir, "routes.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	diags, err := AnalyzePaths([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, d := range diags {
		messages = append(messages, d.Message)
	}
	joined := strings.Join(messages, "\n")
	if strings.Contains(joined, "unsupported policy constructor") {
		t.Fatalf("a registered policy must be supported: %s", joined)
	}
	if !strings.Contains(joined, "identity-bearing") {
		t.Fatalf("a non-identity part must not satisfy the cache rule: %s", joined)
	}
	if !strings.Contains(joined, "forbidden by rule") {
		t.Fatalf("the registered rule must run: %s", joined)
	}
	if !strings.Contains(joined, "cannot appear after") {
		t.Fatalf("stage ordering must apply to extension policies: %s", joined)
	}
	if len(diags) != 3 {
		t.Fatalf("want exactly 3 diagnostics (unsafe cache, forbidden, late), got %d: %s", len(diags), joined)
	}
	if ExtensionHint("Org Rule failed") != "see docs" {
		t.Fatal("extension hints must be found")
	}
}
