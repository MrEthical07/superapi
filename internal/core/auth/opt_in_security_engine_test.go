package auth_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
)

func clearOptInEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"AUTH_LOGIN_IP_LIMITER_ENABLED", "AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS",
		"AUTH_LOGIN_IP_LIMITER_COOLDOWN", "AUTH_REFRESH_RECHECK_USER",
	} {
		t.Setenv(name, "")
	}
}

func fromIP(ip string) context.Context {
	return goauth.WithClientIP(context.Background(), ip)
}

// loginAcrossAccounts makes three wrong-password attempts against three
// different accounts from one address, then logs in to a fourth account with
// its correct password from that same address.
func loginAcrossAccounts(t *testing.T, engine *goauth.Engine, ip string) error {
	t.Helper()
	for _, who := range []string{"a", "b", "c"} {
		if _, _, err := engine.Login(fromIP(ip), who+"@example.com", "wrong-password"); err == nil {
			t.Fatalf("a wrong password logged in as %s", who)
		}
	}
	_, _, err := engine.Login(fromIP(ip), "d@example.com", casPassword)
	return err
}

func newAccounts(t *testing.T, engine *goauth.Engine) {
	t.Helper()
	for _, who := range []string{"a", "b", "c", "d"} {
		createCASAccount(t, engine, who+"@example.com")
	}
}

// With AUTH_LOGIN_IP_LIMITER_ENABLED the failure budget is per client address
// and spans accounts: three failures from one address (budget 3) refuse a
// fourth login from it even for another account with the right password, while
// another address is unaffected. Without the flag nothing is refused.
func TestLoginIPLimiterBudgetSpansAccounts(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		clearOptInEnv(t)
		t.Setenv("AUTH_LOGIN_IP_LIMITER_ENABLED", "true")
		t.Setenv("AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "3")
		engine, _, _ := newCASEngine(t, auth.Features{})
		newAccounts(t, engine)

		err := loginAcrossAccounts(t, engine, "203.0.113.9")
		if !errors.Is(err, goauth.ErrLoginRateLimited) {
			t.Fatalf("fourth login from the exhausted address: err=%v, want ErrLoginRateLimited", err)
		}
		if _, _, err := engine.Login(fromIP("198.51.100.7"), "d@example.com", casPassword); err != nil {
			t.Fatalf("login from another address: %v", err)
		}
	})

	t.Run("a login with no address is not counted", func(t *testing.T) {
		clearOptInEnv(t)
		t.Setenv("AUTH_LOGIN_IP_LIMITER_ENABLED", "true")
		t.Setenv("AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "3")
		engine, _, _ := newCASEngine(t, auth.Features{})
		newAccounts(t, engine)

		for _, who := range []string{"a", "b", "c"} {
			_, _, _ = engine.Login(context.Background(), who+"@example.com", "wrong-password")
		}
		if _, _, err := engine.Login(context.Background(), "d@example.com", casPassword); err != nil {
			t.Fatalf("login without a client address: %v", err)
		}
	})

	t.Run("disabled by default", func(t *testing.T) {
		clearOptInEnv(t)
		engine, _, _ := newCASEngine(t, auth.Features{})
		newAccounts(t, engine)

		if err := loginAcrossAccounts(t, engine, "203.0.113.9"); err != nil {
			t.Fatalf("with the limiter off the fourth login must succeed, got %v", err)
		}
	})
}

// userStore lets a test switch the user lookup off to simulate a database
// outage, and counts lookups by id.
type userStore struct {
	*auth.StoreUserProvider
	down    atomic.Bool
	idReads atomic.Int64
}

func (s *userStore) GetUserByID(userID string) (goauth.UserRecord, error) {
	s.idReads.Add(1)
	if s.down.Load() {
		return goauth.UserRecord{}, errors.New("database unavailable")
	}
	return s.StoreUserProvider.GetUserByID(userID)
}

func newRefreshEngine(t *testing.T) (*goauth.Engine, *authtest.UserRepository, *userStore) {
	t.Helper()
	users := authtest.NewUserRepository()
	store := &userStore{}
	engine, _ := authtest.NewEngineWith(t, users, auth.Features{}, func(base *auth.StoreUserProvider) goauth.UserProvider {
		store.StoreUserProvider = base
		return store
	})
	return engine, users, store
}

func loginTokens(t *testing.T, engine *goauth.Engine, identifier string) (userID, refreshToken string) {
	t.Helper()
	userID = createCASAccount(t, engine, identifier)
	_, refresh, err := engine.Login(context.Background(), identifier, casPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return userID, refresh
}

// With AUTH_REFRESH_RECHECK_USER a refresh re-reads the user: a change to the
// stored account version (any status transition, TOTP enable/disable) or a
// blocking status ends the session at the next refresh, and an unreachable
// store fails the refresh instead of letting it succeed. Without the flag
// refresh reads nothing and none of that is noticed.
func TestRefreshRecheckUser(t *testing.T) {
	t.Run("account version change refuses the refresh", func(t *testing.T) {
		clearOptInEnv(t)
		t.Setenv("AUTH_REFRESH_RECHECK_USER", "true")
		engine, users, _ := newRefreshEngine(t)
		userID, refresh := loginTokens(t, engine, "e@example.com")

		// A status write to the same value still advances account_version,
		// which is how SuperAPI reports a change to the account.
		if _, err := users.UpdateStatus(context.Background(), userID, "active"); err != nil {
			t.Fatalf("bump account version: %v", err)
		}
		if _, _, err := engine.Refresh(context.Background(), refresh); !errors.Is(err, goauth.ErrSessionNotFound) {
			t.Fatalf("refresh after an account version change: err=%v, want ErrSessionNotFound", err)
		}
	})

	t.Run("disabled account refuses the refresh", func(t *testing.T) {
		clearOptInEnv(t)
		t.Setenv("AUTH_REFRESH_RECHECK_USER", "true")
		engine, users, _ := newRefreshEngine(t)
		userID, refresh := loginTokens(t, engine, "f@example.com")

		if _, err := users.UpdateStatus(context.Background(), userID, "disabled"); err != nil {
			t.Fatalf("disable: %v", err)
		}
		if _, _, err := engine.Refresh(context.Background(), refresh); !errors.Is(err, goauth.ErrAccountDisabled) {
			t.Fatalf("refresh of a disabled account: err=%v, want ErrAccountDisabled", err)
		}
	})

	t.Run("unchanged user still refreshes, with one read", func(t *testing.T) {
		clearOptInEnv(t)
		t.Setenv("AUTH_REFRESH_RECHECK_USER", "true")
		engine, _, store := newRefreshEngine(t)
		_, refresh := loginTokens(t, engine, "g@example.com")

		before := store.idReads.Load()
		if _, _, err := engine.Refresh(context.Background(), refresh); err != nil {
			t.Fatalf("refresh of an unchanged user: %v", err)
		}
		if reads := store.idReads.Load() - before; reads != 1 {
			t.Fatalf("refresh read the user %d time(s), want exactly 1", reads)
		}
	})

	t.Run("store outage fails the refresh as unavailable", func(t *testing.T) {
		clearOptInEnv(t)
		t.Setenv("AUTH_REFRESH_RECHECK_USER", "true")
		engine, _, store := newRefreshEngine(t)
		_, refresh := loginTokens(t, engine, "h@example.com")

		store.down.Store(true)
		if _, _, err := engine.Refresh(context.Background(), refresh); !errors.Is(err, goauth.ErrSystemUnavailable) {
			t.Fatalf("refresh during a store outage: err=%v, want ErrSystemUnavailable", err)
		}
		// The refresh token was not spent: it works once the store is back.
		store.down.Store(false)
		if _, _, err := engine.Refresh(context.Background(), refresh); err != nil {
			t.Fatalf("refresh after the store recovered: %v", err)
		}
	})

	t.Run("off by default: no read, change not noticed, outage ignored", func(t *testing.T) {
		clearOptInEnv(t)
		engine, users, store := newRefreshEngine(t)
		userID, refresh := loginTokens(t, engine, "i@example.com")

		before := store.idReads.Load()
		if _, err := users.UpdateStatus(context.Background(), userID, "active"); err != nil {
			t.Fatalf("bump account version: %v", err)
		}
		store.down.Store(true)
		if _, _, err := engine.Refresh(context.Background(), refresh); err != nil {
			t.Fatalf("refresh with the recheck off: %v", err)
		}
		if reads := store.idReads.Load() - before; reads != 0 {
			t.Fatalf("refresh read the user %d time(s) with the recheck off, want 0", reads)
		}
	})
}
