package auth_test

import (
	"context"
	"errors"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
)

const tenancyTestPassword = "correct-horse-battery-staple"

func TestProjectGoAuthConfigNoDeprecatedTenantLint(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg, err := auth.ProjectGoAuthConfig(auth.ModeHybrid, auth.TenancySettings{Enabled: enabled}, auth.Features{})
		if err != nil {
			t.Fatalf("config (tenancy=%v): %v", enabled, err)
		}
		if cfg.MultiTenant.Enabled != enabled {
			t.Fatalf("MultiTenant.Enabled=%v want %v", cfg.MultiTenant.Enabled, enabled)
		}
		if cfg.MultiTenant.EnforceIsolation { //nolint:staticcheck // asserting the deprecated field stays unset
			t.Fatal("EnforceIsolation must stay unset (deprecated no-op in goAuth v0.5.0)")
		}
		for _, w := range cfg.Lint() {
			switch w.Code {
			case "tenant_enforce_isolation_noop", "tenant_header_noop", "account_duplicate_identifier_provider_owned":
				t.Fatalf("unexpected deprecated-field lint %q (tenancy=%v)", w.Code, enabled)
			}
		}
	}
}

// plainProvider hides the TenantAwareUserProvider methods to prove goAuth
// refuses to build a multi-tenant engine without them.
type plainProvider struct{ goauth.UserProvider }

func TestBuildWithTenancyEnabled(t *testing.T) {
	// Succeeds with the store provider (implements TenantAwareUserProvider).
	authtest.NewEngine(t, true, authtest.NewUserRepository())

	// Fails fast when the capability is missing.
	_, _, err := auth.NewGoAuthEngine(authtest.NewRedis(t), auth.ModeHybrid, auth.TenancySettings{Enabled: true}, auth.Features{},
		plainProvider{auth.NewStoreUserProvider(authtest.NewUserRepository())})
	if err == nil {
		t.Fatal("expected Build to fail for a provider without TenantAwareUserProvider")
	}
}

func createTestAccount(t *testing.T, engine *goauth.Engine, tenantID, identifier string) string {
	t.Helper()
	ctx := context.Background()
	if tenantID != "" {
		ctx = auth.WithRequestTenant(ctx, tenantID)
	}
	res, err := engine.CreateAccount(ctx, goauth.CreateAccountRequest{Identifier: identifier, Password: tenancyTestPassword})
	if err != nil {
		t.Fatalf("create account %s in tenant %q: %v", identifier, tenantID, err)
	}
	return res.UserID
}

func TestCrossTenantLoginIsIndistinguishableFromWrongPassword(t *testing.T) {
	repo := authtest.NewUserRepository()
	engine, _ := authtest.NewEngine(t, true, repo)
	createTestAccount(t, engine, "tenant-a", "alice@example.com")

	ctxA := auth.WithRequestTenant(context.Background(), "tenant-a")
	ctxB := auth.WithRequestTenant(context.Background(), "tenant-b")

	access, _, err := engine.Login(ctxA, "alice@example.com", tenancyTestPassword)
	if err != nil {
		t.Fatalf("same-tenant login: %v", err)
	}
	result, err := engine.Validate(ctxA, access, goauth.ModeInherit)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if result.TenantID != "tenant-a" {
		t.Fatalf("token tenant=%q want tenant-a", result.TenantID)
	}

	_, _, crossErr := engine.Login(ctxB, "alice@example.com", tenancyTestPassword)
	if crossErr == nil {
		t.Fatal("cross-tenant login must fail")
	}
	_, _, wrongErr := engine.Login(ctxA, "alice@example.com", "wrong-password-value")
	if wrongErr == nil {
		t.Fatal("wrong-password login must fail")
	}
	_, _, unknownErr := engine.Login(ctxB, "nobody@example.com", tenancyTestPassword)
	if unknownErr == nil {
		t.Fatal("unknown-user login must fail")
	}

	if !errors.Is(crossErr, goauth.ErrInvalidCredentials) || !errors.Is(wrongErr, goauth.ErrInvalidCredentials) || !errors.Is(unknownErr, goauth.ErrInvalidCredentials) {
		t.Fatalf("errors must all be ErrInvalidCredentials: cross=%v wrong=%v unknown=%v", crossErr, wrongErr, unknownErr)
	}
	if crossErr.Error() != wrongErr.Error() || crossErr.Error() != unknownErr.Error() {
		t.Fatalf("error text differs: cross=%q wrong=%q unknown=%q", crossErr, wrongErr, unknownErr)
	}
}

func TestTenantAwareLookupsDoNotCrossTenants(t *testing.T) {
	repo := authtest.NewUserRepository()
	engine, provider := authtest.NewEngine(t, true, repo)
	userID := createTestAccount(t, engine, "tenant-a", "bob@example.com")
	ctx := context.Background()

	if rec, err := provider.GetUserByIDInTenant(ctx, "tenant-a", userID); err != nil || rec.TenantID != "tenant-a" {
		t.Fatalf("same-tenant id lookup: rec=%+v err=%v", rec, err)
	}
	cases := []struct{ name, tenant, id, ident string }{
		{"id other tenant", "tenant-b", userID, ""},
		{"id empty tenant", "", userID, ""},
		{"identifier other tenant", "tenant-b", "", "bob@example.com"},
		{"identifier empty tenant", "", "", "bob@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.id != "" {
				_, err = provider.GetUserByIDInTenant(ctx, tc.tenant, tc.id)
			} else {
				_, err = provider.GetUserByIdentifierInTenant(ctx, tc.tenant, tc.ident)
			}
			if !errors.Is(err, goauth.ErrUserNotFound) {
				t.Fatalf("err=%v want ErrUserNotFound", err)
			}
		})
	}
}

func TestTenancyOffKeepsV080Behavior(t *testing.T) {
	repo := authtest.NewUserRepository()
	engine, provider := authtest.NewEngine(t, false, repo)
	userID := createTestAccount(t, engine, "", "carol@example.com")

	stored, err := repo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("stored user: %v", err)
	}
	if stored.TenantID != auth.DefaultTenantID {
		t.Fatalf("stored tenant=%q want default %q", stored.TenantID, auth.DefaultTenantID)
	}

	rec, err := provider.GetUserByID(userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if rec.TenantID != "" {
		t.Fatalf("tenancy off must leave UserRecord.TenantID empty, got %q", rec.TenantID)
	}

	access, _, err := engine.Login(context.Background(), "carol@example.com", tenancyTestPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	result, err := engine.Validate(context.Background(), access, goauth.ModeInherit)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	// goAuth stamps its default tenant "0" when no tenant is attached. This is
	// the v0.8.0 (goAuth v0.4.0) behavior and must not change.
	if result.TenantID != auth.DefaultTenantID {
		t.Fatalf("principal tenant=%q want %q (v0.8.0 behavior)", result.TenantID, auth.DefaultTenantID)
	}
}
