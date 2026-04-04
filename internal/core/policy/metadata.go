package policy

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// PolicyType classifies policy behavior for validation and diagnostics.
type PolicyType string

const (
	// PolicyTypeUnknown marks unclassified policy metadata.
	PolicyTypeUnknown PolicyType = "unknown"
	// PolicyTypeNoop marks no-op wrapper policies.
	PolicyTypeNoop PolicyType = "noop"
	// PolicyTypeRequireJSON marks content-type enforcement policy.
	PolicyTypeRequireJSON PolicyType = "require_json"
	// PolicyTypeWithHeader marks static header injection policy.
	PolicyTypeWithHeader PolicyType = "with_header"
	// PolicyTypeCacheControl marks explicit Cache-Control response policy.
	PolicyTypeCacheControl PolicyType = "cache_control"
	// PolicyTypeAuthRequired marks authentication enforcement policy.
	PolicyTypeAuthRequired PolicyType = "auth_required"
	// PolicyTypeRequirePerm marks all-of permission enforcement policy.
	PolicyTypeRequirePerm PolicyType = "require_perm"
	// PolicyTypeRequireAnyPerm marks any-of permission enforcement policy.
	PolicyTypeRequireAnyPerm PolicyType = "require_any_perm"
	// PolicyTypeTenantRequired marks tenant scope enforcement policy.
	PolicyTypeTenantRequired PolicyType = "tenant_required"
	// PolicyTypeTenantMatchFromPath marks path-tenant isolation policy.
	PolicyTypeTenantMatchFromPath PolicyType = "tenant_match_from_path"
	// PolicyTypeRateLimit marks route throttling policy.
	PolicyTypeRateLimit PolicyType = "rate_limit"
	// PolicyTypeCacheRead marks cache read/write policy.
	PolicyTypeCacheRead PolicyType = "cache_read"
	// PolicyTypeCacheInvalidate marks cache invalidation policy.
	PolicyTypeCacheInvalidate PolicyType = "cache_invalidate"
	// PolicyTypeCustom marks user-defined annotated policies.
	PolicyTypeCustom PolicyType = "custom"
)

// CacheReadMetadata stores safety flags needed for route validation.
type CacheReadMetadata struct {
	// AllowAuthenticated indicates route opted into authenticated caching.
	AllowAuthenticated bool
	// VaryByUserID indicates cache key varies by user ID.
	VaryByUserID bool
	// VaryByTenantID indicates cache key varies by tenant ID.
	VaryByTenantID bool
}

// CacheInvalidateMetadata stores cache invalidation policy details.
type CacheInvalidateMetadata struct {
	// TagSpecCount is the number of invalidation tag specs configured.
	TagSpecCount int
}

// Metadata captures validator-relevant attributes of one policy.
type Metadata struct {
	// Type is the policy classifier.
	Type PolicyType
	// Name is human-readable policy name for diagnostics.
	Name string
	// TenantPathParam is the tenant route parameter for tenant-match policy.
	TenantPathParam string
	// CacheRead holds cache-read safety metadata.
	CacheRead CacheReadMetadata
	// CacheInvalidate holds cache invalidation metadata.
	CacheInvalidate CacheInvalidateMetadata
}

var policyMetadata sync.Map

func annotatePolicy(p Policy, meta Metadata) Policy {
	if p == nil {
		return nil
	}
	if meta.Type == "" {
		meta.Type = PolicyTypeUnknown
	}
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = string(meta.Type)
	}
	policyMetadata.Store(policyPointer(p), meta)
	return p
}

// AnnotateCustom annotates custom policy metadata for validator compatibility.
func AnnotateCustom(name string, p Policy) Policy {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = "custom"
	}
	return annotatePolicy(p, Metadata{Type: PolicyTypeCustom, Name: trimmed})
}

// DescribePolicy returns policy metadata when the policy is annotated.
func DescribePolicy(p Policy) (Metadata, bool) {
	if p == nil {
		return Metadata{}, false
	}
	v, ok := policyMetadata.Load(policyPointer(p))
	if !ok {
		return Metadata{}, false
	}
	meta, ok := v.(Metadata)
	return meta, ok
}

// DescribePolicies returns metadata list for all policies or an error if any policy is invalid.
func DescribePolicies(policies ...Policy) ([]Metadata, error) {
	metas := make([]Metadata, 0, len(policies))
	for i, p := range policies {
		if p == nil {
			return nil, fmt.Errorf("nil policy at index %d", i)
		}
		meta, ok := DescribePolicy(p)
		if !ok {
			return nil, fmt.Errorf("policy at index %d is not annotated", i)
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func policyPointer(p Policy) uintptr {
	return reflect.ValueOf(p).Pointer()
}
