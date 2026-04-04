package ratelimit

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

func TestKeyByTokenHashDeterministicAndSafe(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/system/whoami", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token")

	keyer := KeyByTokenHash(12)
	scope1, id1 := keyer(req)
	scope2, id2 := keyer(req)

	if scope1 != ScopeToken || scope2 != ScopeToken {
		t.Fatalf("unexpected scope: %q %q", scope1, scope2)
	}
	if id1 != id2 {
		t.Fatalf("expected deterministic hash id, got %q and %q", id1, id2)
	}
	if strings.Contains(id1, "super-secret-token") {
		t.Fatalf("token hash leaked raw token: %q", id1)
	}
	if len(id1) != 12 {
		t.Fatalf("id length=%d want=12", len(id1))
	}
}

func TestResolveScopeAndIdentifierAutoPrefersUser(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/system/whoami", nil)
	ctx := auth.WithContext(req.Context(), auth.AuthContext{UserID: "u-123", TenantID: "t-456"})
	req = req.WithContext(ctx)

	scope, id := ResolveScopeAndIdentifier(req, Rule{Scope: ScopeAuto})
	if scope != ScopeUser {
		t.Fatalf("scope=%q want=%q", scope, ScopeUser)
	}
	if id != "u-123" {
		t.Fatalf("id=%q want=%q", id, "u-123")
	}
}

func TestRedisLimiterFixedWindowBlocksAfterLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	limiter, err := NewRedisLimiter(client, Config{Env: "test", FailOpen: true})
	if err != nil {
		t.Fatalf("NewRedisLimiter() error = %v", err)
	}

	req := Request{
		Route:      "/api/v1/system/whoami",
		Scope:      ScopeUser,
		Identifier: "u1",
		Limit:      2,
		Window:     time.Minute,
	}

	first, err := limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("Allow(first) error = %v", err)
	}
	if !first.Allowed {
		t.Fatalf("expected first request allowed")
	}

	second, err := limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("Allow(second) error = %v", err)
	}
	if !second.Allowed {
		t.Fatalf("expected second request allowed")
	}

	third, err := limiter.Allow(context.Background(), req)
	if err != nil {
		t.Fatalf("Allow(third) error = %v", err)
	}
	if third.Allowed {
		t.Fatalf("expected third request blocked")
	}
	if third.RetryAfter <= 0 {
		t.Fatalf("expected retry after > 0, got %s", third.RetryAfter)
	}

	keys := mr.Keys()
	if len(keys) != 1 {
		t.Fatalf("keys=%v want exactly one key", keys)
	}
	if !strings.Contains(keys[0], "rl:test:/api/v1/system/whoami:user:u1") {
		t.Fatalf("unexpected key format: %q", keys[0])
	}
}

func TestRedisLimiterFailOpenOnRedisError(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  5 * time.Millisecond,
		ReadTimeout:  5 * time.Millisecond,
		WriteTimeout: 5 * time.Millisecond,
	})
	t.Cleanup(func() { _ = client.Close() })

	limiter, err := NewRedisLimiter(client, Config{Env: "test", FailOpen: true})
	if err != nil {
		t.Fatalf("NewRedisLimiter() error = %v", err)
	}

	decision, err := limiter.Allow(context.Background(), Request{
		Route:      "/api/v1/system/whoami",
		Scope:      ScopeAnon,
		Identifier: "anonymous",
		Limit:      1,
		Window:     time.Second,
	})
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected fail-open decision to allow")
	}
	if decision.Outcome != OutcomeFailOpen {
		t.Fatalf("outcome=%q want=%q", decision.Outcome, OutcomeFailOpen)
	}
}

func TestInt64ToIntBoundedSaturation(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want int
	}{
		{"at max", maxIntBound, int(maxIntBound)},
		{"at min", minIntBound, int(minIntBound)},
		{"zero", 0, 0},
		{"positive", 42, 42},
		{"negative", -42, -42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := int64ToIntBounded(tc.in)
			if got != tc.want {
				t.Fatalf("int64ToIntBounded(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestAllowRemainingDoesNotOverflowOnLargeCurrentCount(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	limiter, err := NewRedisLimiter(client, Config{Env: "test", FailOpen: true})
	if err != nil {
		t.Fatalf("NewRedisLimiter() error = %v", err)
	}

	req := Request{
		Route:      "/api/v1/overflow",
		Scope:      ScopeUser,
		Identifier: "u-overflow",
		Limit:      1,
		Window:     time.Minute,
	}

	// Call Allow enough times to exceed the limit; verify Remaining is always non-negative.
	for i := 0; i < 5; i++ {
		d, err := limiter.Allow(context.Background(), req)
		if err != nil {
			t.Fatalf("Allow() error = %v", err)
		}
		if d.Remaining < 0 {
			t.Fatalf("Remaining=%d is negative after %d calls", d.Remaining, i+1)
		}
	}
}

func TestRedisLimiterFailClosedOnRedisError(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  5 * time.Millisecond,
		ReadTimeout:  5 * time.Millisecond,
		WriteTimeout: 5 * time.Millisecond,
	})
	t.Cleanup(func() { _ = client.Close() })

	limiter, err := NewRedisLimiter(client, Config{Env: "test", FailOpen: false})
	if err != nil {
		t.Fatalf("NewRedisLimiter() error = %v", err)
	}

	decision, err := limiter.Allow(context.Background(), Request{
		Route:      "/api/v1/system/whoami",
		Scope:      ScopeAnon,
		Identifier: "anonymous",
		Limit:      1,
		Window:     time.Second,
	})
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected fail-closed decision to block")
	}
	if decision.Outcome != OutcomeError {
		t.Fatalf("outcome=%q want=%q", decision.Outcome, OutcomeError)
	}
}
