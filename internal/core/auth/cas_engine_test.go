package auth_test

import (
	"context"
	"sync"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
	goauthpassword "github.com/MrEthical07/goAuth/password"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
)

const casPassword = "correct-horse-battery-staple"

// casRecorder decorates the core provider and records which write goAuth chose
// for the three flows that have a compare-and-swap path: rehash-on-login,
// email-verification activation and automatic lockout. Embedding promotes every
// other method and optional interface unchanged. before, when set, runs just
// before a compare-and-swap reaches the store; tests use it to land a competing
// write in the window the swap exists to close.
type casRecorder struct {
	*auth.StoreUserProvider

	mu            sync.Mutex
	passwordCAS   []passwordCASCall
	statusCAS     []statusCASCall
	plainPassword int
	plainStatus   int

	beforePasswordCAS func()
	beforeStatusCAS   func()
}

type passwordCASCall struct{ userID, expected, next string }

type statusCASCall struct {
	userID         string
	expected, next goauth.AccountStatus
}

var (
	_ goauth.PasswordHashCASUpdater  = (*casRecorder)(nil)
	_ goauth.AccountStatusCASUpdater = (*casRecorder)(nil)
)

func (r *casRecorder) CompareAndSwapPasswordHash(ctx context.Context, scope, userID, expectedOldHash, newHash string) (bool, error) {
	r.mu.Lock()
	r.passwordCAS = append(r.passwordCAS, passwordCASCall{userID: userID, expected: expectedOldHash, next: newHash})
	hook := r.beforePasswordCAS
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return r.StoreUserProvider.CompareAndSwapPasswordHash(ctx, scope, userID, expectedOldHash, newHash)
}

func (r *casRecorder) UpdateAccountStatusIf(ctx context.Context, scope, userID string, expected, next goauth.AccountStatus) (bool, error) {
	r.mu.Lock()
	r.statusCAS = append(r.statusCAS, statusCASCall{userID: userID, expected: expected, next: next})
	hook := r.beforeStatusCAS
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return r.StoreUserProvider.UpdateAccountStatusIf(ctx, scope, userID, expected, next)
}

func (r *casRecorder) UpdatePasswordHash(userID, newHash string) error {
	r.mu.Lock()
	r.plainPassword++
	r.mu.Unlock()
	return r.StoreUserProvider.UpdatePasswordHash(userID, newHash)
}

func (r *casRecorder) UpdateAccountStatus(ctx context.Context, userID string, status goauth.AccountStatus) (goauth.UserRecord, error) {
	r.mu.Lock()
	r.plainStatus++
	r.mu.Unlock()
	return r.StoreUserProvider.UpdateAccountStatus(ctx, userID, status)
}

func (r *casRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.passwordCAS, r.statusCAS = nil, nil
	r.plainPassword, r.plainStatus = 0, 0
}

func newCASEngine(t *testing.T, features auth.Features, mutators ...auth.ConfigMutator) (*goauth.Engine, *authtest.UserRepository, *casRecorder) {
	t.Helper()
	users := authtest.NewUserRepository()
	rec := &casRecorder{}
	engine, _ := authtest.NewEngineWith(t, users, features, func(base *auth.StoreUserProvider) goauth.UserProvider {
		rec.StoreUserProvider = base
		return rec
	}, mutators...)
	return engine, users, rec
}

func createCASAccount(t *testing.T, engine *goauth.Engine, identifier string) string {
	t.Helper()
	res, err := engine.CreateAccount(context.Background(), goauth.CreateAccountRequest{Identifier: identifier, Password: casPassword})
	if err != nil {
		t.Fatalf("create account %s: %v", identifier, err)
	}
	return res.UserID
}

// weakHash is casPassword hashed under cheaper Argon2 parameters than the
// engine's, as if the stored hash predated a cost increase.
func weakHash(t *testing.T) string {
	t.Helper()
	weak, err := goauthpassword.NewArgon2(goauthpassword.Config{Memory: 8192, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	if err != nil {
		t.Fatalf("weak hasher: %v", err)
	}
	h, err := weak.Hash(casPassword)
	if err != nil {
		t.Fatalf("weak hash: %v", err)
	}
	return h
}

// goAuth detects both capabilities by type assertion at Build, so a decorator
// that exposes them is enough for the engine to use them. The core provider
// itself must satisfy them (asserted at compile time in provider_store.go).
func TestCoreProviderImplementsCASCapabilities(t *testing.T) {
	var p any = auth.NewStoreUserProvider(authtest.NewUserRepository())
	if _, ok := p.(goauth.PasswordHashCASUpdater); !ok {
		t.Fatal("StoreUserProvider must implement goauth.PasswordHashCASUpdater")
	}
	if _, ok := p.(goauth.AccountStatusCASUpdater); !ok {
		t.Fatal("StoreUserProvider must implement goauth.AccountStatusCASUpdater")
	}
}

// Rehash-on-login goes through CompareAndSwapPasswordHash with the verified
// (old) hash, never through the unconditional by-id write.
func TestRehashOnLoginUsesCompareAndSwap(t *testing.T) {
	engine, users, rec := newCASEngine(t, auth.Features{})
	userID := createCASAccount(t, engine, "ivy@example.com")

	old := weakHash(t)
	if err := users.UpdatePasswordHash(context.Background(), userID, old); err != nil {
		t.Fatalf("seed weak hash: %v", err)
	}
	rec.reset()

	if _, _, err := engine.Login(context.Background(), "ivy@example.com", casPassword); err != nil {
		t.Fatalf("login: %v", err)
	}

	if len(rec.passwordCAS) != 1 || rec.passwordCAS[0].userID != userID || rec.passwordCAS[0].expected != old || rec.passwordCAS[0].next == old {
		t.Fatalf("password CAS calls = %+v, want one swap of the verified hash for a new one", rec.passwordCAS)
	}
	if rec.plainPassword != 0 {
		t.Fatalf("rehash used the unconditional UpdatePasswordHash %d time(s)", rec.plainPassword)
	}
	stored, err := users.GetByID(context.Background(), userID)
	if err != nil || stored.PasswordHash != rec.passwordCAS[0].next {
		t.Fatalf("hash was not upgraded: err=%v", err)
	}
}

// A password change that lands between the login's read and the rehash write
// must survive: the swap reports not-applied and the new password stays.
func TestRehashOnLoginDoesNotOverwriteConcurrentPasswordChange(t *testing.T) {
	engine, users, rec := newCASEngine(t, auth.Features{})
	userID := createCASAccount(t, engine, "jay@example.com")
	if err := users.UpdatePasswordHash(context.Background(), userID, weakHash(t)); err != nil {
		t.Fatalf("seed weak hash: %v", err)
	}
	rec.reset()

	const changed = "$argon2id$concurrent-password-change"
	rec.beforePasswordCAS = func() {
		if err := users.UpdatePasswordHash(context.Background(), userID, changed); err != nil {
			t.Errorf("concurrent change: %v", err)
		}
	}

	if _, _, err := engine.Login(context.Background(), "jay@example.com", casPassword); err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(rec.passwordCAS) != 1 {
		t.Fatalf("password CAS calls = %+v, want exactly one", rec.passwordCAS)
	}
	stored, err := users.GetByID(context.Background(), userID)
	if err != nil || stored.PasswordHash != changed {
		t.Fatalf("stored hash = %q (err=%v), want the concurrent change %q kept", stored.PasswordHash, err, changed)
	}
}

// Confirming an email address activates a pending account through
// UpdateAccountStatusIf(Pending, Active).
func TestEmailVerificationUsesStatusCompareAndSwap(t *testing.T) {
	engine, users, rec := newCASEngine(t, auth.Features{EmailVerification: true})
	userID := createCASAccount(t, engine, "kim@example.com")
	rec.reset()

	challenge, err := engine.RequestEmailVerification(context.Background(), "kim@example.com")
	if err != nil || challenge == "" {
		t.Fatalf("request verification: %q, %v", challenge, err)
	}
	if err := engine.ConfirmEmailVerification(context.Background(), challenge); err != nil {
		t.Fatalf("confirm verification: %v", err)
	}

	want := statusCASCall{userID: userID, expected: goauth.AccountPendingVerification, next: goauth.AccountActive}
	if len(rec.statusCAS) != 1 || rec.statusCAS[0] != want {
		t.Fatalf("status CAS calls = %+v, want exactly %+v", rec.statusCAS, want)
	}
	if rec.plainStatus != 0 {
		t.Fatalf("activation used the unconditional UpdateAccountStatus %d time(s)", rec.plainStatus)
	}
	stored, err := users.GetByID(context.Background(), userID)
	if err != nil || stored.Status != "active" {
		t.Fatalf("status = %q (err=%v), want active", stored.Status, err)
	}
}

// A verification in flight while an administrator disables the account must not
// make it active again.
func TestEmailVerificationDoesNotReactivateDisabledAccount(t *testing.T) {
	engine, users, rec := newCASEngine(t, auth.Features{EmailVerification: true})
	userID := createCASAccount(t, engine, "lou@example.com")

	challenge, err := engine.RequestEmailVerification(context.Background(), "lou@example.com")
	if err != nil || challenge == "" {
		t.Fatalf("request verification: %q, %v", challenge, err)
	}
	rec.reset()
	rec.beforeStatusCAS = func() {
		if _, err := users.UpdateStatus(context.Background(), userID, "disabled"); err != nil {
			t.Errorf("concurrent disable: %v", err)
		}
	}
	_ = engine.ConfirmEmailVerification(context.Background(), challenge)

	stored, err := users.GetByID(context.Background(), userID)
	if err != nil || stored.Status != "disabled" {
		t.Fatalf("status = %q (err=%v), want the concurrent disable kept", stored.Status, err)
	}
}

func lockoutConfig(c *goauth.Config) {
	c.Security.AutoLockoutEnabled = true
	c.Security.AutoLockoutThreshold = 3
}

// With auto-lockout on, the lock is written Active -> Locked through
// UpdateAccountStatusIf.
func TestAutoLockoutUsesStatusCompareAndSwap(t *testing.T) {
	engine, users, rec := newCASEngine(t, auth.Features{}, lockoutConfig)
	userID := createCASAccount(t, engine, "max@example.com")
	rec.reset()

	for i := 0; i < 3; i++ {
		if _, _, err := engine.Login(context.Background(), "max@example.com", "wrong-password-"+string(rune('a'+i))); err == nil {
			t.Fatal("a wrong password logged in")
		}
	}

	want := statusCASCall{userID: userID, expected: goauth.AccountActive, next: goauth.AccountLocked}
	if len(rec.statusCAS) != 1 || rec.statusCAS[0] != want {
		t.Fatalf("status CAS calls = %+v, want exactly %+v", rec.statusCAS, want)
	}
	if rec.plainStatus != 0 {
		t.Fatalf("lock used the unconditional UpdateAccountStatus %d time(s)", rec.plainStatus)
	}
	stored, err := users.GetByID(context.Background(), userID)
	if err != nil || stored.Status != "locked" {
		t.Fatalf("status = %q (err=%v), want locked", stored.Status, err)
	}
}

// A disable that lands between the login's status read and the lock write is
// not overwritten with Locked.
func TestAutoLockoutDoesNotOverwriteConcurrentDisable(t *testing.T) {
	engine, users, rec := newCASEngine(t, auth.Features{}, lockoutConfig)
	userID := createCASAccount(t, engine, "ned@example.com")
	rec.reset()
	rec.beforeStatusCAS = func() {
		if _, err := users.UpdateStatus(context.Background(), userID, "disabled"); err != nil {
			t.Errorf("concurrent disable: %v", err)
		}
	}

	for i := 0; i < 3; i++ {
		_, _, _ = engine.Login(context.Background(), "ned@example.com", "wrong-password-"+string(rune('a'+i)))
	}
	if len(rec.statusCAS) != 1 {
		t.Fatalf("status CAS calls = %+v, want exactly one", rec.statusCAS)
	}
	stored, err := users.GetByID(context.Background(), userID)
	if err != nil || stored.Status != "disabled" {
		t.Fatalf("status = %q (err=%v), want the concurrent disable kept", stored.Status, err)
	}
}
