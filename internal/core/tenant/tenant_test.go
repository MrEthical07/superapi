package tenant

import (
	"net/http"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

func TestTenantIDFromContext(t *testing.T) {
	ctx := auth.WithContext(t.Context(), auth.AuthContext{UserID: "u1", TenantID: "t1"})
	tenantID, ok := TenantIDFromContext(ctx)
	if !ok {
		t.Fatalf("expected tenant id from context")
	}
	if tenantID != "t1" {
		t.Fatalf("tenantID=%q want=%q", tenantID, "t1")
	}
}

func TestRequireTenantMissing(t *testing.T) {
	err := RequireTenant(t.Context())
	if err == nil {
		t.Fatalf("expected tenant required error")
	}
	ae := err.Error()
	if ae == "" {
		t.Fatalf("expected non-empty error")
	}
}

func TestRequireTenantPresent(t *testing.T) {
	ctx := auth.WithContext(t.Context(), auth.AuthContext{TenantID: "t1"})
	if err := RequireTenant(ctx); err != nil {
		t.Fatalf("RequireTenant() error = %v", err)
	}
}

func TestIsSameTenant(t *testing.T) {
	if !IsSameTenant("t1", "t1") {
		t.Fatalf("expected same tenant")
	}
	if IsSameTenant("t1", "t2") {
		t.Fatalf("expected mismatch")
	}
	if IsSameTenant("", "t1") {
		t.Fatalf("expected empty principal tenant to fail")
	}
}

func TestRequireTenantErrorShape(t *testing.T) {
	err := RequireTenant(t.Context())
	ae, ok := err.(interface{ Error() string })
	if !ok || ae.Error() == "" {
		t.Fatalf("expected app error compatible error")
	}
	_ = http.StatusForbidden
}
