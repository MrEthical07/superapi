package policy

import (
	"fmt"
	"strings"
)

// WARNING:
// This is core infrastructure code.
// Avoid modifying validation order/rules unless you understand policy execution semantics.
// For policy behavior, see docs/policies.md and docs/cache-guide.md.

// MustValidateRoute validates route policy wiring and panics on invalid config.
//
// Use this during route registration to fail fast on unsafe policy combinations.
func MustValidateRoute(method, pattern string, policies ...Policy) {
	MustValidateRouteWith(nil, method, pattern, policies...)
}

// MustValidateRouteWith is MustValidateRoute plus the route rules optional
// features contribute (see RouteRule).
func MustValidateRouteWith(rules []RouteRule, method, pattern string, policies ...Policy) {
	if err := ValidateRouteWith(rules, method, pattern, policies...); err != nil {
		panicInvalidRouteConfig(err.Error())
	}
}

// ValidateRoute validates route policy wiring and returns descriptive validation errors.
func ValidateRoute(method, pattern string, policies ...Policy) error {
	return ValidateRouteWith(nil, method, pattern, policies...)
}

// ValidateRouteWith is ValidateRoute plus feature route rules.
func ValidateRouteWith(rules []RouteRule, method, pattern string, policies ...Policy) error {
	trimmedMethod := strings.TrimSpace(method)
	if trimmedMethod == "" {
		return fmt.Errorf("http method is required")
	}
	trimmedPattern := strings.TrimSpace(pattern)
	if trimmedPattern == "" {
		return fmt.Errorf("route pattern is required")
	}

	metas, err := DescribePolicies(policies...)
	if err != nil {
		return err
	}

	return ValidateRouteMetadataWith(rules, strings.ToUpper(trimmedMethod), trimmedPattern, metas)
}

// ValidateRouteMetadata validates precomputed policy metadata.
//
// This entrypoint is used by both runtime route registration and static analyzers.
func ValidateRouteMetadata(method, pattern string, metas []Metadata) error {
	return ValidateRouteMetadataWith(nil, method, pattern, metas)
}

// ValidateRouteMetadataWith is ValidateRouteMetadata plus feature route rules.
func ValidateRouteMetadataWith(rules []RouteRule, method, pattern string, metas []Metadata) error {
	trimmedMethod := strings.ToUpper(strings.TrimSpace(method))
	if trimmedMethod == "" {
		return fmt.Errorf("http method is required")
	}
	trimmedPattern := strings.TrimSpace(pattern)
	if trimmedPattern == "" {
		return fmt.Errorf("route pattern is required")
	}
	if len(metas) == 0 {
		return nil
	}

	return validateRouteRules(trimmedMethod, trimmedPattern, metas, rules)
}

func panicInvalidRouteConfig(message string) {
	message = strings.TrimSpace(message)
	panic(fmt.Sprintf("invalid route config: %s", strings.TrimSpace(message)))
}

func panicInvalidRouteConfigf(format string, args ...any) {
	panicInvalidRouteConfig(fmt.Sprintf(format, args...))
}
