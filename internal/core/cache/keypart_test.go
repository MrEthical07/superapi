package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

// orgPart is a feature-style key part: the value comes from a principal
// attribute, exactly as an optional feature would contribute one.
func orgPart() KeyPart {
	return KeyPart{
		Name:     "org",
		Identity: true,
		Extract: func(_ *http.Request, p auth.AuthContext) string {
			return p.Attribute("org_id")
		},
	}
}

func requestAs(orgID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
	principal := auth.AuthContext{UserID: "u1", Attributes: []auth.Attribute{{Key: "org_id", Value: orgID}}}
	return r.WithContext(auth.WithContext(context.Background(), principal))
}

func TestReadKeyVariesByCustomPart(t *testing.T) {
	mgr, _ := newTestManager(t)
	cfg := CacheReadConfig{TTL: time.Minute, VaryBy: CacheVaryBy{Parts: []KeyPart{orgPart()}}}

	keyA, err := mgr.BuildReadKey(context.Background(), requestAs("acme"), "/api/v1/things", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey: %v", err)
	}
	keyA2, _ := mgr.BuildReadKey(context.Background(), requestAs("acme"), "/api/v1/things", cfg)
	keyB, _ := mgr.BuildReadKey(context.Background(), requestAs("globex"), "/api/v1/things", cfg)

	if keyA != keyA2 {
		t.Fatalf("same part value must give the same key: %q vs %q", keyA, keyA2)
	}
	if keyA == keyB {
		t.Fatal("different part values must give different keys")
	}

	plain, _ := mgr.BuildReadKey(context.Background(), requestAs("acme"), "/api/v1/things", CacheReadConfig{TTL: time.Minute})
	if plain == keyA {
		t.Fatal("a key with a custom part must differ from one without")
	}
}

func TestTagSpecCustomPartScopesTag(t *testing.T) {
	spec := CacheTagSpec{Name: "thing", Parts: []KeyPart{orgPart()}}

	tagsA, err := ResolveTagNames(requestAs("acme"), PrepareTagSpecs([]CacheTagSpec{spec}))
	if err != nil {
		t.Fatalf("ResolveTagNames: %v", err)
	}
	tagsB, _ := ResolveTagNames(requestAs("globex"), PrepareTagSpecs([]CacheTagSpec{spec}))
	if len(tagsA) != 1 || len(tagsB) != 1 || tagsA[0] == tagsB[0] {
		t.Fatalf("tags must be scoped per part value: %v vs %v", tagsA, tagsB)
	}
	if !strings.Contains(tagsA[0], "org=acme") {
		t.Fatalf("tag %q should carry org=acme", tagsA[0])
	}

	// A tag that cannot be scoped is an error, never an unscoped tag.
	if _, err := ResolveTagNames(requestAs(""), PrepareTagSpecs([]CacheTagSpec{spec})); err == nil || !strings.Contains(err.Error(), "missing org") {
		t.Fatalf("empty part value must fail tag resolution, got %v", err)
	}
}

func TestKeyPartValidateAndIdentity(t *testing.T) {
	for _, bad := range []KeyPart{
		{Name: "", Extract: orgPart().Extract},
		{Name: "has space", Extract: orgPart().Extract},
		{Name: "a=b", Extract: orgPart().Extract},
		{Name: "org"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("Validate(%+v) should fail", bad)
		}
	}
	if err := orgPart().Validate(); err != nil {
		t.Fatalf("valid part rejected: %v", err)
	}
	if HasIdentityPart([]KeyPart{{Name: "x"}}) || !HasIdentityPart([]KeyPart{{Name: "x"}, orgPart()}) {
		t.Fatal("HasIdentityPart must report only identity-bearing parts")
	}
}

// A part value cannot forge another key dimension, and a part without an
// extractor (which policy.CacheRead reports as a configuration error) does not
// panic a direct BuildReadKey caller.
func TestReadKeyPartValuesAreEscapedAndBrokenPartsIgnored(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()
	cfg := CacheReadConfig{TTL: time.Minute, VaryBy: CacheVaryBy{Parts: []KeyPart{orgPart()}}}

	forged, err := mgr.BuildReadKey(ctx, requestAs("acme|user=u1"), "/api/v1/things", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey: %v", err)
	}
	plain, _ := mgr.BuildReadKey(ctx, requestAs("acme"), "/api/v1/things", cfg)
	withUser, _ := mgr.BuildReadKey(ctx, requestAs("acme"), "/api/v1/things", CacheReadConfig{TTL: time.Minute, VaryBy: CacheVaryBy{Parts: []KeyPart{orgPart()}, UserID: true}})
	if forged == plain || forged == withUser {
		t.Fatal("an org value containing separators must not collide with another key")
	}

	broken := CacheReadConfig{TTL: time.Minute, VaryBy: CacheVaryBy{Parts: []KeyPart{{Name: "org"}, {Extract: orgPart().Extract}}}}
	if _, err := mgr.BuildReadKey(ctx, requestAs("acme"), "/api/v1/things", broken); err != nil {
		t.Fatalf("a part without an extractor must be ignored, got %v", err)
	}
}
