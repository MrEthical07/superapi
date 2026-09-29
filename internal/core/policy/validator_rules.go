package policy

import (
	"fmt"
)

// Policy order stages. A policy at a lower stage may not appear after one at a
// higher stage. Built-in policies use them implicitly; a feature policy sets
// Metadata.Stage (StageIsolation is where a feature's isolation checks belong:
// after authentication, before RBAC).
const (
	StageAuth         = 1
	StageIsolation    = 2
	StageRBAC         = 3
	StageRateLimit    = 4
	StageCache        = 5
	StageCacheControl = 6
)

// RouteRule is an extra route check contributed by an optional feature. It
// receives the route and the metadata of its policies and returns a
// descriptive error when the wiring is unsafe.
type RouteRule func(method, pattern string, metas []Metadata) error

func validateRouteRules(method, pattern string, metas []Metadata, rules []RouteRule) error {
	if err := validatePolicyOrdering(metas); err != nil {
		return err
	}
	if err := validateAuthDependencies(metas); err != nil {
		return err
	}
	if err := validateCacheSafety(metas); err != nil {
		return err
	}
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		if err := rule(method, pattern, metas); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicyOrdering(metas []Metadata) error {
	previousStage := 0
	previousType := PolicyTypeUnknown

	for _, meta := range metas {
		stage := policyOrderStage(meta)
		if stage > 0 {
			if stage < previousStage {
				return fmt.Errorf("policy %s cannot appear after %s", meta.Name, previousType)
			}
			previousStage = stage
			previousType = meta.Type
		}
	}

	return nil
}

func validateAuthDependencies(metas []Metadata) error {
	hasAuthRequired := hasPolicyType(metas, PolicyTypeAuthRequired)
	if hasAuthRequired {
		return nil
	}

	if hasPolicyType(metas, PolicyTypeRequirePerm) ||
		hasPolicyType(metas, PolicyTypeRequireAnyPerm) {
		return fmt.Errorf("%s is required when RBAC policies are configured", PolicyTypeAuthRequired)
	}

	return nil
}

func validateCacheSafety(metas []Metadata) error {
	if !hasPolicyType(metas, PolicyTypeAuthRequired) {
		return nil
	}

	cacheReadPolicies := findPolicies(metas, PolicyTypeCacheRead)
	for _, cacheRead := range cacheReadPolicies {
		if !cacheRead.CacheRead.VaryByUserID && !cacheRead.CacheRead.VaryByIdentityPart {
			return fmt.Errorf("%s on authenticated routes requires VaryBy.UserID or an identity-bearing VaryBy.Parts entry", PolicyTypeCacheRead)
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

func policyOrderStage(meta Metadata) int {
	if meta.Stage > 0 {
		return meta.Stage
	}
	switch meta.Type {
	case PolicyTypeAuthRequired:
		return StageAuth
	case PolicyTypeRequirePerm, PolicyTypeRequireAnyPerm:
		return StageRBAC
	case PolicyTypeRateLimit:
		return StageRateLimit
	case PolicyTypeCacheRead, PolicyTypeCacheInvalidate:
		return StageCache
	case PolicyTypeCacheControl:
		return StageCacheControl
	default:
		return 0
	}
}
