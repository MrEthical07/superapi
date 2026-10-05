package tenancy_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
	goauthpassword "github.com/MrEthical07/goAuth/password"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
	"github.com/MrEthical07/superapi/internal/tenancy"
	"github.com/MrEthical07/superapi/internal/tenancy/tenancytest"
)

const newPassword = "a-different-battery-staple-9"

// passwordWrite is one password-hash write that reached the provider: tenant is
// the tenant argument of UpdatePasswordHashInTenant, or "" for the by-id
// UpdatePasswordHash.
type passwordWrite struct {
	inTenant bool
	tenant   string
	userID   string
}

type writeLog struct {
	mu     sync.Mutex
	writes []passwordWrite
}

func (l *writeLog) add(w passwordWrite) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writes = append(l.writes, w)
}

func (l *writeLog) take() []passwordWrite {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.writes
	l.writes = nil
	return out
}

// recordingProvider decorates the tenancy provider and records which of the two
// password writes goAuth chose. Embedding promotes every other method and
// optional interface unchanged.
type recordingProvider struct {
	*tenancy.Provider
	log *writeLog
}

func (p *recordingProvider) UpdatePasswordHash(userID, newHash string) error {
	p.log.add(passwordWrite{userID: userID})
	return p.Provider.UpdatePasswordHash(userID, newHash)
}

func (p *recordingProvider) UpdatePasswordHashInTenant(ctx context.Context, tenantID, userID, newHash string) error {
	p.log.add(passwordWrite{inTenant: true, tenant: tenantID, userID: userID})
	return p.Provider.UpdatePasswordHashInTenant(ctx, tenantID, userID, newHash)
}

// coreRecordingProvider is the same recorder over the core provider, which
// implements only the by-id write.
type coreRecordingProvider struct {
	*auth.StoreUserProvider
	log *writeLog
}

func (p *coreRecordingProvider) UpdatePasswordHash(userID, newHash string) error {
	p.log.add(passwordWrite{userID: userID})
	return p.StoreUserProvider.UpdatePasswordHash(userID, newHash)
}

func recordedEngine(t *testing.T, features auth.Features) (*tenancytest.Engine, *writeLog) {
	t.Helper()
	log := &writeLog{}
	e := tenancytest.NewEngineWith(t, features, func(p *tenancy.Provider) goauth.UserProvider {
		return &recordingProvider{Provider: p, log: log}
	})
	return e, log
}

func expectWrites(t *testing.T, log *writeLog, name string, want ...passwordWrite) {
	t.Helper()
	got := log.take()
	if len(got) != len(want) {
		t.Fatalf("%s: password writes = %+v, want %+v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: password writes = %+v, want %+v", name, got, want)
		}
	}
}

func tenantCtx(tenantID string) context.Context {
	return tenancy.WithRequestTenant(context.Background(), tenantID)
}

// With tenancy on, goAuth writes every new hash through
// UpdatePasswordHashInTenant with the tenant it resolved, never through the
// by-id UpdatePasswordHash: change password, password-reset confirm and
// rehash-on-login.
func TestPasswordWritesAreTenantScoped(t *testing.T) {
	t.Run("change password", func(t *testing.T) {
		e, log := recordedEngine(t, auth.Features{})
		userID := createTestAccount(t, e.Engine, "tenant-a", "gina@example.com")
		log.take()

		ctx := tenantCtx("tenant-a")
		if err := e.ChangePassword(ctx, userID, password, newPassword); err != nil {
			t.Fatalf("change password: %v", err)
		}
		expectWrites(t, log, "change password", passwordWrite{inTenant: true, tenant: "tenant-a", userID: userID})
		if _, _, err := e.Login(ctx, "gina@example.com", newPassword); err != nil {
			t.Fatalf("login with the new password: %v", err)
		}
	})

	t.Run("password reset confirm", func(t *testing.T) {
		e, log := recordedEngine(t, auth.Features{PasswordReset: true})
		userID := createTestAccount(t, e.Engine, "tenant-b", "hal@example.com")
		log.take()

		ctx := tenantCtx("tenant-b")
		challenge, err := e.RequestPasswordReset(ctx, "hal@example.com")
		if err != nil || challenge == "" {
			t.Fatalf("request reset: %q, %v", challenge, err)
		}
		if err := e.ConfirmPasswordReset(ctx, challenge, newPassword); err != nil {
			t.Fatalf("confirm reset: %v", err)
		}
		expectWrites(t, log, "reset confirm", passwordWrite{inTenant: true, tenant: "tenant-b", userID: userID})
		if _, _, err := e.Login(ctx, "hal@example.com", newPassword); err != nil {
			t.Fatalf("login with the reset password: %v", err)
		}
	})

	t.Run("rehash on login", func(t *testing.T) {
		e, log := recordedEngine(t, auth.Features{})
		userID := createTestAccount(t, e.Engine, "tenant-c", "ivy@example.com")

		// Store the password under weaker Argon2 parameters than the engine's,
		// as if the hash predated a cost increase.
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
		log.take()

		ctx := tenantCtx("tenant-c")
		if _, _, err := e.Login(ctx, "ivy@example.com", password); err != nil {
			t.Fatalf("login: %v", err)
		}
		expectWrites(t, log, "rehash on login", passwordWrite{inTenant: true, tenant: "tenant-c", userID: userID})
		stored, err := e.Users.GetByID(context.Background(), userID)
		if err != nil || stored.PasswordHash == weakHash {
			t.Fatalf("hash was not upgraded: err=%v unchanged=%v", err, stored.PasswordHash == weakHash)
		}
	})
}

// A user id from another tenant writes nothing and is reported as not found,
// both at the provider and through the engine, and the owner's password is
// untouched.
func TestCrossTenantPasswordWriteIsRejected(t *testing.T) {
	e, log := recordedEngine(t, auth.Features{PasswordReset: true})
	userA := createTestAccount(t, e.Engine, "tenant-a", "jo@example.com")
	log.take()
	ctxA, ctxB := tenantCtx("tenant-a"), tenantCtx("tenant-b")

	stillOwnedByA := func(name string) {
		t.Helper()
		if _, _, err := e.Login(ctxA, "jo@example.com", password); err != nil {
			t.Fatalf("%s: tenant A's password was disturbed: %v", name, err)
		}
	}

	t.Run("provider", func(t *testing.T) {
		for _, tenantID := range []string{"tenant-b", ""} {
			err := e.Provider.UpdatePasswordHashInTenant(context.Background(), tenantID, userA, "$argon2id$forged")
			if !errors.Is(err, goauth.ErrUserNotFound) {
				t.Fatalf("tenant %q: err = %v, want goauth.ErrUserNotFound", tenantID, err)
			}
		}
		stored, err := e.Users.GetByID(context.Background(), userA)
		if err != nil || stored.PasswordHash == "$argon2id$forged" {
			t.Fatalf("cross-tenant write reached storage: err=%v", err)
		}
		stillOwnedByA("provider")
	})

	t.Run("change password", func(t *testing.T) {
		err := e.ChangePassword(ctxB, userA, password, newPassword)
		if !errors.Is(err, goauth.ErrUserNotFound) {
			t.Fatalf("err = %v, want goauth.ErrUserNotFound", err)
		}
		expectWrites(t, log, "change password across tenants")
		stillOwnedByA("change password")
	})

	t.Run("password reset confirm", func(t *testing.T) {
		challenge, err := e.RequestPasswordReset(ctxA, "jo@example.com")
		if err != nil || challenge == "" {
			t.Fatalf("request reset: %q, %v", challenge, err)
		}
		if err := e.ConfirmPasswordReset(ctxB, challenge, newPassword); err == nil {
			t.Fatal("a reset challenge issued in tenant A was accepted under tenant B")
		}
		expectWrites(t, log, "reset confirm across tenants")
		stillOwnedByA("password reset confirm")
	})
}

// With goAuth's multi-tenant mode off, goAuth never consults the updater, even
// on a provider that implements it, and the core provider (which does not)
// keeps the by-id write.
func TestPasswordWriteWithMultiTenantOff(t *testing.T) {
	t.Run("core provider", func(t *testing.T) {
		var core any = auth.NewStoreUserProvider(authtest.NewUserRepository())
		if _, ok := core.(goauth.TenantAwarePasswordUpdater); ok {
			t.Fatal("the core provider must not implement goauth.TenantAwarePasswordUpdater; only the tenancy wrapper does")
		}

		log := &writeLog{}
		users := authtest.NewUserRepository()
		engine, _ := authtest.NewEngineWith(t, users, auth.Features{}, func(base *auth.StoreUserProvider) goauth.UserProvider {
			return &coreRecordingProvider{StoreUserProvider: base, log: log}
		})
		res, err := engine.CreateAccount(context.Background(), goauth.CreateAccountRequest{Identifier: "kim@example.com", Password: password})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}
		if err := engine.ChangePassword(context.Background(), res.UserID, password, newPassword); err != nil {
			t.Fatalf("change password: %v", err)
		}
		expectWrites(t, log, "core change password", passwordWrite{userID: res.UserID})
	})

	t.Run("wrapper without multi-tenant mode", func(t *testing.T) {
		log := &writeLog{}
		users := authtest.NewUserRepository()
		store := tenancytest.NewStore(users)
		engine, _ := authtest.NewEngineWith(t, users, auth.Features{}, func(base *auth.StoreUserProvider) goauth.UserProvider {
			return &recordingProvider{Provider: tenancy.NewProvider(base, store), log: log}
		})
		res, err := engine.CreateAccount(context.Background(), goauth.CreateAccountRequest{Identifier: "lee@example.com", Password: password})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}
		if err := engine.ChangePassword(context.Background(), res.UserID, password, newPassword); err != nil {
			t.Fatalf("change password: %v", err)
		}
		expectWrites(t, log, "wrapper, multi-tenant off", passwordWrite{userID: res.UserID})
	})
}
