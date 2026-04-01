package policy

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

type PolicyType string

const (
	PolicyTypeUnknown             PolicyType = "unknown"
	PolicyTypeNoop                PolicyType = "noop"
	PolicyTypeRequireJSON         PolicyType = "require_json"
	PolicyTypeWithHeader          PolicyType = "with_header"
	PolicyTypeAuthRequired        PolicyType = "auth_required"
	PolicyTypeRequirePerm         PolicyType = "require_perm"
	PolicyTypeRequireAnyPerm      PolicyType = "require_any_perm"
	PolicyTypeTenantRequired      PolicyType = "tenant_required"
	PolicyTypeTenantMatchFromPath PolicyType = "tenant_match_from_path"
	PolicyTypeRateLimit           PolicyType = "rate_limit"
	PolicyTypeCacheRead           PolicyType = "cache_read"
	PolicyTypeCacheInvalidate     PolicyType = "cache_invalidate"
	PolicyTypeCustom              PolicyType = "custom"
)

type CacheReadMetadata struct {
	AllowAuthenticated bool
	VaryByUserID       bool
	VaryByTenantID     bool
}

type CacheInvalidateMetadata struct {
	TagCount int
}

type Metadata struct {
	Type            PolicyType
	Name            string
	TenantPathParam string
	CacheRead       CacheReadMetadata
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

func AnnotateCustom(name string, p Policy) Policy {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = "custom"
	}
	return annotatePolicy(p, Metadata{Type: PolicyTypeCustom, Name: trimmed})
}

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
