package validator

import (
	"go/ast"
	"strings"

	corepolicy "github.com/MrEthical07/superapi/internal/core/policy"
)

// An optional feature teaches the static verifier about its policies with an
// Extension registered from a file of its own in this package (the feature's
// tenancy_*-style file, which is deleted when the feature is removed). The
// verifier is a build-time tool, so registration happens in init, before any
// analysis runs; nothing here is used by the running server.

// PolicyParser turns a feature policy call in a route declaration into the
// metadata the route rules validate.
type PolicyParser func(call *ast.CallExpr) corepolicy.Metadata

// Extension is what a feature contributes to static verification.
type Extension struct {
	// Policies maps a constructor name (for example "TenantRequired") to its
	// parser.
	Policies map[string]PolicyParser
	// IdentityParts names the cache key part constructors that are
	// identity-bearing (cache.KeyPart with Identity set), for example
	// "CacheVary". A VaryBy.Parts entry calling one satisfies the
	// authenticated-cache rule.
	IdentityParts []string
	// Rules are extra route rules, the same ones the running router applies.
	Rules []corepolicy.RouteRule
	// Hints maps a lower-case fragment of a diagnostic message to advice.
	Hints map[string]string
}

var extensions []Extension

// RegisterExtension adds a feature's static-verification support.
func RegisterExtension(ext Extension) {
	extensions = append(extensions, ext)
}

func extensionPolicy(name string) (PolicyParser, bool) {
	for _, ext := range extensions {
		if parse, ok := ext.Policies[name]; ok {
			return parse, true
		}
	}
	return nil, false
}

func isIdentityPart(name string) bool {
	for _, ext := range extensions {
		for _, n := range ext.IdentityParts {
			if n == name {
				return true
			}
		}
	}
	return false
}

func extensionRules() []corepolicy.RouteRule {
	var rules []corepolicy.RouteRule
	for _, ext := range extensions {
		rules = append(rules, ext.Rules...)
	}
	return rules
}

// ExtensionHint returns feature advice for a diagnostic message, or "".
func ExtensionHint(message string) string {
	normalized := strings.ToLower(message)
	for _, ext := range extensions {
		for fragment, hint := range ext.Hints {
			if strings.Contains(normalized, fragment) {
				return hint
			}
		}
	}
	return ""
}
