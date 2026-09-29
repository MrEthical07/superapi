package auth

import (
	"net/http"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// Identifiers are normalized (trimmed, lower-cased) at the module boundary, so
// one mailbox is one account whatever case the client types.
func TestRegisterThenLoginWithAnyCase(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{RegistrationEnabled: true}})

	if res := h.post("/api/v1/auth/register", map[string]string{"identifier": "  Alice@Example.COM ", "password": testPassword}); res.status != http.StatusAccepted {
		t.Fatalf("register: status=%d body=%s", res.status, res.body)
	}

	for _, id := range []string{"alice@example.com", "ALICE@EXAMPLE.COM", "Alice@Example.com", "  aLiCe@example.com "} {
		token := h.login(id, testPassword)
		who := h.do(call{method: http.MethodGet, path: "/api/v1/auth/whoami", token: token})
		if who.status != http.StatusOK {
			t.Fatalf("whoami after login as %q: status=%d body=%s", id, who.status, who.body)
		}
	}
	if bad := h.post("/api/v1/auth/login", map[string]string{"identifier": "ALICE@example.com", "password": "wrong-password-entirely"}); bad.status != http.StatusUnauthorized {
		t.Fatalf("wrong password with a case variant: status=%d", bad.status)
	}
}

// A registration that differs only in case is the same identifier: it is
// accepted with the same response as a new one (enumeration-safe), creates no
// second account, and does not change the existing account's password.
func TestRegisterCaseVariantIsTreatedAsExisting(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{RegistrationEnabled: true}})
	h.createUser("taken@example.com")

	fresh := h.post("/api/v1/auth/register", map[string]string{"identifier": "new@example.com", "password": testPassword})
	variant := h.post("/api/v1/auth/register", map[string]string{"identifier": "Taken@Example.COM", "password": "attacker-chosen-password-1"})
	if fresh.status != http.StatusAccepted || variant.status != http.StatusAccepted {
		t.Fatalf("fresh=%d variant=%d want 202/202 (%s / %s)", fresh.status, variant.status, fresh.body, variant.body)
	}
	if fresh.comparable(t) != variant.comparable(t) {
		t.Fatalf("a case-variant duplicate is distinguishable: %s vs %s", fresh.comparable(t), variant.comparable(t))
	}

	if res := h.post("/api/v1/auth/login", map[string]string{"identifier": "taken@example.com", "password": "attacker-chosen-password-1"}); res.status == http.StatusOK {
		t.Fatal("the duplicate registration must not create or take over an account")
	}
	h.login("taken@example.com", testPassword)
	h.login("TAKEN@example.com", testPassword)
}

func TestPasswordResetRequestWithCaseVariantReachesTheAccount(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{PasswordResetEnabled: true}})
	h.createUser("reset@example.com")

	res := h.post("/api/v1/auth/password/reset/request", map[string]string{"identifier": "  RESET@Example.com "})
	if res.status != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", res.status, res.body)
	}
	h.flush()
	if challenge, ok := h.notifier.reset("reset@example.com"); !ok || challenge == "" {
		t.Fatal("a reset request typed in another case must be delivered to the stored account")
	}
}

func TestEmailVerificationRequestWithCaseVariantReachesTheAccount(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{
		RegistrationEnabled:       true,
		EmailVerificationEnabled:  true,
		EmailVerificationRequired: true,
	}})
	if res := h.post("/api/v1/auth/register", map[string]string{"identifier": "Pending@Example.com", "password": testPassword}); res.status != http.StatusAccepted {
		t.Fatalf("register: status=%d body=%s", res.status, res.body)
	}
	h.flush()
	if _, ok := h.notifier.verification("pending@example.com"); !ok {
		t.Fatal("registration must send the verification message to the normalized address")
	}

	res := h.post("/api/v1/auth/email/verify/request", map[string]string{"identifier": "PENDING@EXAMPLE.COM"})
	if res.status != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", res.status, res.body)
	}
}

// goAuth's login limiter is keyed by identifier. With the identifier
// normalized, spreading failed attempts across case variants must not multiply
// the attacker's budget.
func TestLoginLimiterIsSharedAcrossCaseVariants(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createUser("victim@example.com")

	variants := []string{"victim@example.com", "Victim@example.com", "VICTIM@example.com", "vIcTiM@Example.com"}
	const attempts = 40
	perVariant := map[string]int{}
	limitedAt := 0
	for i := 0; i < attempts; i++ {
		id := variants[i%len(variants)]
		perVariant[id]++
		res := h.post("/api/v1/auth/login", map[string]string{"identifier": id, "password": "definitely-not-the-password"})
		if res.status == http.StatusTooManyRequests {
			limitedAt = i + 1
			break
		}
		if res.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d (%q): status=%d body=%s", i+1, id, res.status, res.body)
		}
	}
	if limitedAt == 0 {
		t.Fatalf("no rate limit after %d failed attempts across %d case variants", attempts, len(variants))
	}
	// A per-variant budget would allow at least this many attempts in total.
	const perIdentifierBudget = 5
	if limitedAt > perIdentifierBudget*len(variants) {
		t.Fatalf("limited only at attempt %d; case variants got separate budgets", limitedAt)
	}
	for id, n := range perVariant {
		if n > perIdentifierBudget {
			t.Fatalf("variant %q was allowed %d attempts before the limit", id, n)
		}
	}

	// The limit holds for every spelling, including the correct password.
	for _, id := range variants {
		if res := h.post("/api/v1/auth/login", map[string]string{"identifier": id, "password": testPassword}); res.status != http.StatusTooManyRequests {
			t.Fatalf("variant %q after the limit: status=%d, want 429", id, res.status)
		}
	}
}
