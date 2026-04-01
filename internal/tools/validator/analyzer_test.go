package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzePathsDetectsInvalidRoutePolicyChain(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "routes.go")
	source := `package sample
import (
	"net/http"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
)
func register(r httpx.Router, h http.Handler) {
	r.Handle(http.MethodGet, "/api/v1/projects/{id}", h,
		policy.RequirePerm("project.read"),
	)
}
`
	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	diagnostics, err := AnalyzePaths([]string{tempDir})
	if err != nil {
		t.Fatalf("AnalyzePaths() error = %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics")
	}

	found := false
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "auth_required") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auth dependency diagnostic, got: %+v", diagnostics)
	}
}

func TestAnalyzePathsPassesValidRoutePolicyChain(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "routes.go")
	source := `package sample
import (
	"net/http"
	"time"
		goauth "github.com/MrEthical07/goAuth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)
	func register(r httpx.Router, h http.Handler, engine *goauth.Engine, limiter ratelimit.Limiter, cacheManager *cache.Manager) {
	r.Handle(http.MethodGet, "/api/v1/tenants/{tenant_id}/projects", h,
			policy.AuthRequired(engine, "strict"),
		policy.TenantRequired(),
		policy.TenantMatchFromPath("tenant_id"),
		policy.RequirePerm("project.read"),
		policy.RateLimit(limiter, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeTenant}),
		policy.CacheRead(cacheManager, cache.CacheReadConfig{TTL: time.Minute, VaryBy: cache.CacheVaryBy{TenantID: true}}),
	)
}
`
	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	diagnostics, err := AnalyzePaths([]string{tempDir})
	if err != nil {
		t.Fatalf("AnalyzePaths() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}

func TestAnalyzePathsSkipsDynamicPatternWithoutPolicies(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "routes.go")
	source := `package sample
import (
	"net/http"
	"github.com/MrEthical07/superapi/internal/core/httpx"
)
func register(r httpx.Router, h http.Handler, pattern string) {
	r.Handle(http.MethodGet, pattern, h)
}
`
	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	diagnostics, err := AnalyzePaths([]string{tempDir})
	if err != nil {
		t.Fatalf("AnalyzePaths() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}
