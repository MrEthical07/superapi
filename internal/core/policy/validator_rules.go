package policy

import (
	"fmt"
	"strings"
)

func validateRouteRules(method, pattern string, metas []Metadata) error {
	_ = method
	if err := validatePolicyOrdering(metas); err != nil {
		return err
	}
	if err := validateAuthDependencies(metas); err != nil {
		return err
	}
	if err := validateTenantRules(pattern, metas); err != nil {
		return err
	}
	if err := validateCacheSafety(metas); err != nil {
		return err
	}
	return nil
}

func validatePolicyOrdering(metas []Metadata) error {
	previousStage := 0
	previousType := PolicyTypeUnknown
	tenantRequiredIndex := -1
	tenantMatchIndex := -1

	for i, meta := range metas {
		stage := policyOrderStage(meta.Type)
		if stage > 0 {
			if stage < previousStage {
				return fmt.Errorf("policy %s cannot appear after %s", meta.Name, previousType)
			}
			previousStage = stage
			previousType = meta.Type
		}

		switch meta.Type {
		case PolicyTypeTenantRequired:
			if tenantRequiredIndex == -1 {
				tenantRequiredIndex = i
			}
		case PolicyTypeTenantMatchFromPath:
			if tenantMatchIndex == -1 {
				tenantMatchIndex = i
			}
		}
	}

	if tenantRequiredIndex >= 0 && tenantMatchIndex >= 0 && tenantMatchIndex < tenantRequiredIndex {
		return fmt.Errorf("policy %s must appear after %s", PolicyTypeTenantMatchFromPath, PolicyTypeTenantRequired)
	}

	return nil
}

func validateAuthDependencies(metas []Metadata) error {
	hasAuthRequired := hasPolicyType(metas, PolicyTypeAuthRequired)
	if hasAuthRequired {
		return nil
	}

	if hasPolicyType(metas, PolicyTypeRequirePerm) ||
		hasPolicyType(metas, PolicyTypeRequireAnyPerm) ||
		hasPolicyType(metas, PolicyTypeTenantRequired) {
		return fmt.Errorf("%s is required when RBAC or tenant policies are configured", PolicyTypeAuthRequired)
	}

	return nil
}

func validateTenantRules(pattern string, metas []Metadata) error {
	hasTenantRequired := hasPolicyType(metas, PolicyTypeTenantRequired)
	tenantMatchPolicies := findPolicies(metas, PolicyTypeTenantMatchFromPath)

	if len(tenantMatchPolicies) > 0 && !hasTenantRequired {
		return fmt.Errorf("%s requires %s", PolicyTypeTenantMatchFromPath, PolicyTypeTenantRequired)
	}

	if patternContainsTenantID(pattern) {
		if !hasTenantRequired {
			return fmt.Errorf("route %s requires %s", pattern, PolicyTypeTenantRequired)
		}
		if len(tenantMatchPolicies) == 0 {
			return fmt.Errorf("route %s requires %s", pattern, PolicyTypeTenantMatchFromPath)
		}
		for _, tenantMatch := range tenantMatchPolicies {
			if normalizePathParam(tenantMatch.TenantPathParam) != tenantIDParam {
				return fmt.Errorf("%s for route %s must use path param %q", PolicyTypeTenantMatchFromPath, pattern, tenantIDParam)
			}
		}
	}

	return nil
}

func validateCacheSafety(metas []Metadata) error {
	if !hasPolicyType(metas, PolicyTypeAuthRequired) {
		return nil
	}

	cacheReadPolicies := findPolicies(metas, PolicyTypeCacheRead)
	for _, cacheRead := range cacheReadPolicies {
		if !cacheRead.CacheRead.VaryByUserID && !cacheRead.CacheRead.VaryByTenantID {
			return fmt.Errorf("%s on authenticated routes requires VaryBy.UserID or VaryBy.TenantID", PolicyTypeCacheRead)
		}
	}

	return nil
}

func hasPolicyType(metas []Metadata, policyType PolicyType) bool {
	for _, meta := range metas {
		if meta.Type == policyType {
			return true
		}
	}
	return false
}

func findPolicies(metas []Metadata, policyType PolicyType) []Metadata {
	matches := make([]Metadata, 0, len(metas))
	for _, meta := range metas {
		if meta.Type == policyType {
			matches = append(matches, meta)
		}
	}
	return matches
}

func policyOrderStage(policyType PolicyType) int {
	switch policyType {
	case PolicyTypeAuthRequired:
		return 1
	case PolicyTypeTenantRequired, PolicyTypeTenantMatchFromPath:
		return 2
	case PolicyTypeRequirePerm, PolicyTypeRequireAnyPerm:
		return 3
	case PolicyTypeRateLimit:
		return 4
	case PolicyTypeCacheRead, PolicyTypeCacheInvalidate:
		return 5
	case PolicyTypeCacheControl:
		return 6
	default:
		return 0
	}
}

const tenantIDParam = "tenant_id"

func patternContainsTenantID(pattern string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(pattern)), "{"+tenantIDParam+"}")
}

func normalizePathParam(param string) string {
	return strings.ToLower(strings.TrimSpace(param))
}
