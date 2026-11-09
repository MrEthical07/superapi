package tenancy_test

import (
	"context"
	"sync"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
	goauthpassword "github.com/MrEthical07/goAuth/password"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/tenancy"
	"github.com/MrEthical07/superapi/internal/tenancy/tenancytest"
)

// casLog records the compare-and-swap calls that reach the tenancy provider,
// with the tenant goAuth passed.
type casLog struct {
	mu       sync.Mutex
	password []passwordCAS
	status   []statusCAS
	// beforeStatus runs just before a status swap reaches the store, to land a
	// competing write in the window the swap exists to close.
	beforeStatus func()
}

type passwordCAS struct{ tenant, userID, expected, next string }

type statusCAS struct {
	tenant, userID string
	expected, next goauth.AccountStatus
}

type casProvider struct {
	*tenancy.Provider
	log *casLog
}

var (
	_ goauth.PasswordHashCASUpdater  = (*casProvider)(nil)
	_ goauth.AccountStatusCASUpdater = (*casProvider)(nil)
)

func (p *casProvider) CompareAndSwapPasswordHash(ctx context.Context, tenantID, userID, expected, next string) (bool, error) {
	p.log.mu.Lock()
	p.log.password = append(p.log.password, passwordCAS{tenant: tenantID, userID: userID, expected: expected, next: next})
	p.log.mu.Unlock()
	return p.Provider.CompareAndSwapPasswordHash(ctx, tenantID, userID, expected, next)
}

func (p *casProvider) UpdateAccountStatusIf(ctx context.Context, tenantID, userID string, expected, next goauth.AccountStatus) (bool, error) {
	p.log.mu.Lock()
	p.log.status = append(p.log.status, statusCAS{tenant: tenantID, userID: userID, expected: expected, next: next})
	hook := p.log.beforeStatus
	p.log.mu.Unlock()
	if hook != nil {
		hook()
	}
	return p.Provider.UpdateAccountStatusIf(ctx, tenantID, userID, expected, next)
}

func casEngine(t *testing.T, features auth.Features) (*tenancytest.Engine, *casLog) {
	t.Helper()
	log := &casLog{}
	e := tenancytest.NewEngineWith(t, features, func(p *tenancy.Provider) goauth.UserProvider {
		return &casProvider{Provider: p, log: log}
	})
	return e, log
}

// Rehash-on-login upgrades the hash with a compare-and-swap scoped to the
// tenant goAuth resolved, and the upgrade lands for that tenant's user.
func TestRehashOnLoginIsATenantScopedCompareAndSwap(t *testing.T) {
	e, log := casEngine(t, auth.Features{})
	userID := createTestAccount(t, e.Engine, "tenant-c", "ivy@example.com")

	weak, err := goauthpassword.NewArgon2(goauthpassword.Config{Memory: 8192, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	if err != nil {
		t.Fatalf("weak hasher: %v", err)
	}
	weakHash, err := weak.Hash(password)
	if err != nil {
		t.Fatalf("weak hash: %v", err)
	}
	if err := e.Users.UpdatePasswordHash(context.Background(), userID, weakHash); err != nil {
		t.Fatalf("seed weak hash: %v", err)
	}

	if _, _, err := e.Login(tenantCtx("tenant-c"), "ivy@example.com", password); err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(log.password) != 1 {
		t.Fatalf("password CAS calls = %+v, want exactly one", log.password)
	}
	call := log.password[0]
	if call.tenant != "tenant-c" || call.userID != userID || call.expected != weakHash || call.next == weakHash {
		t.Fatalf("password CAS = %+v, want tenant-c, %s, expected the weak hash and a new one", call, userID)
	}
	stored, err := e.Users.GetByID(context.Background(), userID)
	if err != nil || stored.PasswordHash != call.next {
		t.Fatalf("hash was not upgraded: err=%v", err)
	}
}

// Email verification activates a pending account through
// UpdateAccountStatusIf(Pending, Active) in the account's tenant, and a disable
// that lands first is kept.
func TestEmailVerificationUsesTenantScopedStatusCompareAndSwap(t *testing.T) {
	t.Run("activates", func(t *testing.T) {
		e, log := casEngine(t, auth.Features{EmailVerification: true})
		userID := createTestAccount(t, e.Engine, "tenant-a", "kim@example.com")
		ctx := tenantCtx("tenant-a")

		challenge, err := e.RequestEmailVerification(ctx, "kim@example.com")
		if err != nil || challenge == "" {
			t.Fatalf("request verification: %q, %v", challenge, err)
		}
		if err := e.ConfirmEmailVerification(ctx, challenge); err != nil {
			t.Fatalf("confirm verification: %v", err)
		}
		want := statusCAS{tenant: "tenant-a", userID: userID, expected: goauth.AccountPendingVerification, next: goauth.AccountActive}
		if len(log.status) != 1 || log.status[0] != want {
			t.Fatalf("status CAS calls = %+v, want exactly %+v", log.status, want)
		}
		if stored, err := e.Users.GetByID(context.Background(), userID); err != nil || stored.Status != "active" {
			t.Fatalf("status = %q (err=%v), want active", stored.Status, err)
		}
	})

	t.Run("keeps a concurrent disable", func(t *testing.T) {
		e, log := casEngine(t, auth.Features{EmailVerification: true})
		userID := createTestAccount(t, e.Engine, "tenant-a", "lou@example.com")
		ctx := tenantCtx("tenant-a")

		challenge, err := e.RequestEmailVerification(ctx, "lou@example.com")
		if err != nil || challenge == "" {
			t.Fatalf("request verification: %q, %v", challenge, err)
		}
		log.beforeStatus = func() {
			if _, err := e.Users.UpdateStatus(context.Background(), userID, "disabled"); err != nil {
				t.Errorf("concurrent disable: %v", err)
			}
		}
		_ = e.ConfirmEmailVerification(ctx, challenge)
		if stored, err := e.Users.GetByID(context.Background(), userID); err != nil || stored.Status != "disabled" {
			t.Fatalf("status = %q (err=%v), want the concurrent disable kept", stored.Status, err)
		}
	})
}

// A user of another tenant, an empty tenant, and a stale expected value all
// leave the row untouched and report not applied, without an error. The
// in-memory store mirrors the SQL contract proved against Postgres in
// TestUserStoreCompareAndSwapIsTenantScoped.
func TestProviderCompareAndSwapAcrossTenants(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	userA := createTestAccount(t, e.Engine, "tenant-a", "jo@example.com")
	ctx := context.Background()

	stored, err := e.Users.GetByID(ctx, userA)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	oldHash := stored.PasswordHash

	for _, tenantID := range []string{"tenant-b", ""} {
		applied, err := e.Provider.CompareAndSwapPasswordHash(ctx, tenantID, userA, oldHash, "$argon2id$forged")
		if err != nil || applied {
			t.Fatalf("password swap in tenant %q: applied=%v err=%v, want (false, nil)", tenantID, applied, err)
		}
		applied, err = e.Provider.UpdateAccountStatusIf(ctx, tenantID, userA, goauth.AccountActive, goauth.AccountLocked)
		if err != nil || applied {
			t.Fatalf("status swap in tenant %q: applied=%v err=%v, want (false, nil)", tenantID, applied, err)
		}
	}
	after, _ := e.Users.GetByID(ctx, userA)
	if after.PasswordHash != oldHash || after.Status != "active" || after.AccountVersion != stored.AccountVersion {
		t.Fatalf("a cross-tenant swap changed the user: before %+v after %+v", stored, after)
	}

	if applied, err := e.Provider.CompareAndSwapPasswordHash(ctx, "tenant-a", userA, oldHash, "$argon2id$new"); err != nil || !applied {
		t.Fatalf("same-tenant password swap: applied=%v err=%v", applied, err)
	}
	if applied, err := e.Provider.UpdateAccountStatusIf(ctx, "tenant-a", userA, goauth.AccountActive, goauth.AccountLocked); err != nil || !applied {
		t.Fatalf("same-tenant status swap: applied=%v err=%v", applied, err)
	}
}
