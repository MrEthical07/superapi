package tenancy_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
	"github.com/MrEthical07/superapi/internal/tenancy"
	"github.com/MrEthical07/superapi/internal/tenancy/tenancytest"
)

const password = "correct-horse-battery-staple"

func TestEnableMultiTenantNoDeprecatedLint(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		var mutators []auth.ConfigMutator
		if enabled {
			mutators = append(mutators, tenancy.EnableMultiTenant)
		}
		cfg, err := auth.ProjectGoAuthConfig(auth.ModeHybrid, auth.Features{}, mutators...)
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

func TestBuildWithMultiTenantMode(t *testing.T) {
	// Succeeds with the tenant wrapper (implements TenantAwareUserProvider).
	tenancytest.NewEngine(t, auth.Features{})

	// The core provider alone is not enough: the engine must refuse to build.
	_, _, err := auth.NewGoAuthEngine(authtest.NewRedis(t), auth.ModeHybrid, auth.Features{},
		plainProvider{auth.NewStoreUserProvider(authtest.NewUserRepository())}, tenancy.EnableMultiTenant)
	if err == nil {
		t.Fatal("expected Build to fail for a provider without TenantAwareUserProvider")
	}
}

func createTestAccount(t *testing.T, engine *goauth.Engine, tenantID, identifier string) string {
	t.Helper()
	ctx := context.Background()
	if tenantID != "" {
		ctx = tenancy.WithRequestTenant(ctx, tenantID)
	}
	res, err := engine.CreateAccount(ctx, goauth.CreateAccountRequest{Identifier: identifier, Password: password})
	if err != nil {
		t.Fatalf("create account %s in tenant %q: %v", identifier, tenantID, err)
	}
	return res.UserID
}

func TestCrossTenantLoginIsIndistinguishableFromWrongPassword(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	createTestAccount(t, e.Engine, "tenant-a", "alice@example.com")

	ctxA := tenancy.WithRequestTenant(context.Background(), "tenant-a")
	ctxB := tenancy.WithRequestTenant(context.Background(), "tenant-b")

	access, _, err := e.Login(ctxA, "alice@example.com", password)
	if err != nil {
		t.Fatalf("same-tenant login: %v", err)
	}
	result, err := e.Validate(ctxA, access, goauth.ModeInherit)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if result.TenantID != "tenant-a" {
		t.Fatalf("token tenant=%q want tenant-a", result.TenantID)
	}

	_, _, crossErr := e.Login(ctxB, "alice@example.com", password)
	if crossErr == nil {
		t.Fatal("cross-tenant login must fail")
	}
	_, _, wrongErr := e.Login(ctxA, "alice@example.com", "wrong-password-value")
	if wrongErr == nil {
		t.Fatal("wrong-password login must fail")
	}
	_, _, unknownErr := e.Login(ctxB, "nobody@example.com", password)
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
	e := tenancytest.NewEngine(t, auth.Features{})
	userID := createTestAccount(t, e.Engine, "tenant-a", "bob@example.com")
	ctx := context.Background()

	if rec, err := e.Provider.GetUserByIDInTenant(ctx, "tenant-a", userID); err != nil || rec.TenantID != "tenant-a" {
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
				_, err = e.Provider.GetUserByIDInTenant(ctx, tc.tenant, tc.id)
			} else {
				_, err = e.Provider.GetUserByIdentifierInTenant(ctx, tc.tenant, tc.ident)
			}
			if !errors.Is(err, goauth.ErrUserNotFound) {
				t.Fatalf("err=%v want ErrUserNotFound", err)
			}
		})
	}
}

// The tenant-blind lookups and status updates still report which tenant a user
// belongs to, as the pre-move provider did with tenancy on.
func TestTenantBlindLookupsCarryTheTenant(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	userID := createTestAccount(t, e.Engine, "tenant-a", "carol@example.com")

	byID, err := e.Provider.GetUserByID(userID)
	if err != nil || byID.TenantID != "tenant-a" {
		t.Fatalf("GetUserByID: rec=%+v err=%v", byID, err)
	}
	byIdent, err := e.Provider.GetUserByIdentifier("carol@example.com")
	if err != nil || byIdent.TenantID != "tenant-a" {
		t.Fatalf("GetUserByIdentifier: rec=%+v err=%v", byIdent, err)
	}
	updated, err := e.Provider.UpdateAccountStatus(context.Background(), userID, goauth.AccountLocked)
	if err != nil || updated.TenantID != "tenant-a" || updated.Status != goauth.AccountLocked {
		t.Fatalf("UpdateAccountStatus: rec=%+v err=%v", updated, err)
	}
	if _, err := e.Provider.UpdateAccountStatus(context.Background(), "missing", goauth.AccountLocked); !errors.Is(err, goauth.ErrUserNotFound) {
		t.Fatalf("unknown user: err=%v", err)
	}
}

// A user the provider is asked to create without a tenant lands in goAuth's
// default tenant. (goAuth itself refuses CreateAccount without one in
// multi-tenant mode; this covers the provider contract.)
func TestCreateUserWithoutTenantUsesDefault(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	created, err := e.Provider.CreateUser(context.Background(), goauth.CreateUserInput{
		Identifier:   "dave@example.com",
		PasswordHash: "hash",
		Status:       goauth.AccountActive,
	})
	if err != nil || created.TenantID != tenancy.DefaultTenantID {
		t.Fatalf("created=%+v err=%v, want tenant %q", created, err, tenancy.DefaultTenantID)
	}
	if _, err := e.Provider.CreateUser(context.Background(), goauth.CreateUserInput{Identifier: "dave@example.com", PasswordHash: "h", TenantID: "other"}); !errors.Is(err, goauth.ErrProviderDuplicateIdentifier) {
		t.Fatalf("a duplicate identifier is refused across tenants (global uniqueness), got %v", err)
	}
}

// --- id-keyed MFA calls are unreachable across tenants ---

// totpCode computes an RFC 6238 code (SHA1, 6 digits, 30s).
func totpCode(t *testing.T, secretBase32 string) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secretBase32, "=")))
	if err != nil {
		t.Fatalf("decode totp secret: %v", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

// Core MFA SQL no longer filters by tenant, on the strength of goAuth
// resolving the user through the tenant-scoped lookup before every id-keyed
// provider call. That holds for every path SuperAPI uses except the ones the
// provider wrapper covers itself (bare backup-code consumption, WebAuthn
// list/remove). This test proves the end result: a user id from tenant A, used
// under a tenant B request context, is rejected for TOTP setup, confirm and
// disable and for backup-code generate, regenerate and consume, and tenant A's
// state is untouched afterwards. (WebAuthn list/remove: provider_webauthn_test.go.)
func TestCrossTenantMFAIsRejected(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{TOTP: true, TOTPIssuer: "SuperAPI Test"})
	userA := createTestAccount(t, e.Engine, "tenant-a", "erin@example.com")
	ctxA := tenancy.WithRequestTenant(context.Background(), "tenant-a")
	ctxB := tenancy.WithRequestTenant(context.Background(), "tenant-b")

	// Tenant A enrols TOTP and holds backup codes.
	setup, err := e.GenerateTOTPSetup(ctxA, userA)
	if err != nil {
		t.Fatalf("setup in own tenant: %v", err)
	}
	if err := e.ConfirmTOTPSetup(ctxA, userA, totpCode(t, setup.SecretBase32)); err != nil {
		t.Fatalf("confirm in own tenant: %v", err)
	}
	codes, err := e.GenerateBackupCodes(ctxA, userA)
	if err != nil || len(codes) == 0 {
		t.Fatalf("backup codes in own tenant: %v (%d)", err, len(codes))
	}

	rejected := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: a user id from another tenant must be rejected", name)
		}
	}
	_, err = e.GenerateTOTPSetup(ctxB, userA)
	rejected("TOTP setup", err)
	rejected("TOTP confirm", e.ConfirmTOTPSetup(ctxB, userA, totpCode(t, setup.SecretBase32)))
	rejected("TOTP disable", e.DisableTOTP(ctxB, userA))
	_, err = e.GenerateBackupCodes(ctxB, userA)
	rejected("backup codes generate", err)
	_, err = e.RegenerateBackupCodes(ctxB, userA, totpCode(t, setup.SecretBase32))
	rejected("backup codes regenerate", err)
	rejected("backup code consume", e.VerifyBackupCode(ctxB, userA, codes[0]))
	rejected("backup code consume (explicit tenant)", e.VerifyBackupCodeInTenant(ctxB, "tenant-b", userA, codes[0]))

	// The provider itself refuses too, whatever reached it.
	if ok, err := e.Provider.ConsumeBackupCode(ctxB, userA, [32]byte{1}); err != nil || ok {
		t.Errorf("provider.ConsumeBackupCode across tenants = %v, %v; want false, nil", ok, err)
	}

	// Nothing of tenant A's changed: TOTP is still on and the first backup
	// code is still unused, so it can be consumed in its own tenant.
	if err := e.VerifyBackupCode(ctxA, userA, codes[0]); err != nil {
		t.Fatalf("tenant A's backup code was disturbed: %v", err)
	}
	if err := e.DisableTOTP(ctxA, userA); err != nil {
		t.Fatalf("tenant A can still manage its own TOTP: %v", err)
	}
}
