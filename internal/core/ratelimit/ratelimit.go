package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/netx"
)

// Scope identifies which principal dimension rate limiting should key by.
type Scope string

const (
	// ScopeAuto resolves to user, tenant, token, then anon fallback.
	ScopeAuto Scope = "auto"
	// ScopeAnon keys all anonymous traffic together.
	ScopeAnon Scope = "anon"
	// ScopeIP keys by resolved client IP.
	ScopeIP Scope = "ip"
	// ScopeUser keys by authenticated user ID.
	ScopeUser Scope = "user"
	// ScopeTenant keys by authenticated tenant ID.
	ScopeTenant Scope = "tenant"
	// ScopeToken keys by hashed bearer token fingerprint.
	ScopeToken Scope = "token"
)

// Outcome describes limiter decision category for metrics and diagnostics.
type Outcome string

const (
	// OutcomeAllowed indicates request is within budget.
	OutcomeAllowed Outcome = "allowed"
	// OutcomeBlocked indicates budget exceeded.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeFailOpen indicates dependency failure with fail-open behavior.
	OutcomeFailOpen Outcome = "fail_open"
	// OutcomeError indicates dependency failure with fail-closed behavior.
	OutcomeError Outcome = "error"
)

// Rule defines route-level throttling parameters.
type Rule struct {
	// Limit defines max requests per window.
	Limit int
	// Window defines throttling window duration.
	Window time.Duration
	// Scope selects identity strategy used for keys.
	Scope Scope
	// Keyer optionally overrides scope/identifier extraction.
	Keyer Keyer
}

// Validate ensures rule is usable by runtime limiter.
func (r Rule) Validate() error {
	if r.Limit <= 0 {
		return fmt.Errorf("rate limit must be > 0")
	}
	if r.Window <= 0 {
		return fmt.Errorf("rate limit window must be > 0")
	}
	return nil
}

// Request is the normalized limiter input sent to Redis limiter.
type Request struct {
	// Route is low-cardinality route pattern.
	Route string
	// Scope is resolved identity scope.
	Scope Scope
	// Identifier is normalized identity value for the scope.
	Identifier string
	// Limit is request budget for this route/scope.
	Limit int
	// Window is throttling window duration.
	Window time.Duration
}

// Decision describes the limiter result for one request.
type Decision struct {
	// Allowed indicates whether request should proceed.
	Allowed bool
	// Remaining is best-effort remaining budget.
	Remaining int
	// RetryAfter is retry delay when blocked.
	RetryAfter time.Duration
	// Outcome classifies decision for observability.
	Outcome Outcome
}

// Limiter is the runtime interface used by policy middleware.
type Limiter interface {
	Allow(ctx context.Context, req Request) (Decision, error)
}

// ObserveFunc records limiter outcomes for metrics integration.
type ObserveFunc func(route string, outcome Outcome)

// Config configures RedisLimiter defaults.
type Config struct {
	// Env namespaces redis keys by environment.
	Env string
	// FailOpen allows requests when redis calls fail.
	FailOpen bool
	// Observe receives decision outcomes for metrics.
	Observe ObserveFunc
}

// RedisLimiter executes fixed-window rate limiting using Redis and Lua script.
type RedisLimiter struct {
	client   redis.UniversalClient
	script   *redis.Script
	env      string
	failOpen bool
	observe  ObserveFunc
}

var fixedWindowScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])

local current = redis.call("INCR", key)
if current == 1 then
  redis.call("PEXPIRE", key, window_ms)
end

local ttl = redis.call("PTTL", key)
if ttl < 0 then
  ttl = window_ms
end

local allowed = 0
if current <= limit then
  allowed = 1
end

return {allowed, current, ttl}
`)

// NewRedisLimiter constructs a Redis-backed limiter instance.
func NewRedisLimiter(client redis.UniversalClient, cfg Config) (*RedisLimiter, error) {
	if client == nil {
		return nil, fmt.Errorf("ratelimit requires redis client")
	}
	env := strings.TrimSpace(cfg.Env)
	if env == "" {
		env = "dev"
	}
	return &RedisLimiter{
		client:   client,
		script:   fixedWindowScript,
		env:      env,
		failOpen: cfg.FailOpen,
		observe:  cfg.Observe,
	}, nil
}

// Allow applies fixed-window decision logic for one request.
func (l *RedisLimiter) Allow(ctx context.Context, req Request) (Decision, error) {
	if err := validateRequest(req); err != nil {
		return Decision{}, err
	}

	key := BuildKey(l.env, req.Route, req.Scope, req.Identifier)
	windowMS := req.Window.Milliseconds()

	raw, err := l.script.Run(ctx, l.client, []string{key}, req.Limit, windowMS).Result()
	if err != nil {
		if l.failOpen {
			decision := Decision{Allowed: true, Remaining: 0, RetryAfter: 0, Outcome: OutcomeFailOpen}
			l.observeDecision(req.Route, decision.Outcome)
			return decision, nil
		}
		decision := Decision{Allowed: false, Remaining: 0, RetryAfter: req.Window, Outcome: OutcomeError}
		l.observeDecision(req.Route, decision.Outcome)
		return decision, nil
	}

	values, ok := raw.([]interface{})
	if !ok || len(values) != 3 {
		if l.failOpen {
			decision := Decision{Allowed: true, Outcome: OutcomeFailOpen}
			l.observeDecision(req.Route, decision.Outcome)
			return decision, nil
		}
		decision := Decision{Allowed: false, RetryAfter: req.Window, Outcome: OutcomeError}
		l.observeDecision(req.Route, decision.Outcome)
		return decision, nil
	}

	allowed := toInt64(values[0]) == 1
	current := toInt64(values[1])
	ttl := toInt64(values[2])
	if current < 0 {
		current = 0
	}

	remaining := int64(req.Limit) - current
	if remaining < 0 {
		remaining = 0
	}

	decision := Decision{
		Allowed:   allowed,
		Remaining: int64ToIntBounded(remaining),
		Outcome:   OutcomeAllowed,
	}
	if !allowed {
		decision.Outcome = OutcomeBlocked
		if ttl > 0 {
			decision.RetryAfter = time.Duration(ttl) * time.Millisecond
		}
		if decision.RetryAfter <= 0 {
			decision.RetryAfter = req.Window
		}
	}

	l.observeDecision(req.Route, decision.Outcome)
	return decision, nil
}

func (l *RedisLimiter) observeDecision(route string, outcome Outcome) {
	if l == nil || l.observe == nil {
		return
	}
	r := normalizeRoute(route)
	l.observe(r, outcome)
}

// BuildKey builds a normalized redis key for limiter counters.
func BuildKey(env, route string, scope Scope, id string) string {
	normalizedEnv := strings.TrimSpace(env)
	if normalizedEnv == "" {
		normalizedEnv = "dev"
	}
	normalizedRoute := normalizeRoute(route)
	normalizedScope := scope
	if normalizedScope == "" || normalizedScope == ScopeAuto {
		normalizedScope = ScopeAnon
	}
	normalizedID := sanitizeIdentifier(id)
	if normalizedID == "" {
		normalizedID = "anonymous"
	}

	return fmt.Sprintf("rl:%s:%s:%s:%s", normalizedEnv, normalizedRoute, normalizedScope, normalizedID)
}

// ResolveScopeAndIdentifier resolves request identity based on rule and auth context.
func ResolveScopeAndIdentifier(r *http.Request, rule Rule) (Scope, string) {
	if r == nil {
		return ScopeAnon, "anonymous"
	}

	if rule.Keyer != nil {
		scope, id := rule.Keyer(r)
		scope, id = normalizeKey(scope, id)
		if scope != ScopeAnon {
			return scope, id
		}
	}

	switch rule.Scope {
	case ScopeIP:
		return normalizeKey(KeyByIP()(r))
	case ScopeUser:
		if scope, id := normalizeKey(KeyByUser()(r)); scope != ScopeAnon {
			return scope, id
		}
		if scope, id := normalizeKey(KeyByTokenHash(16)(r)); scope != ScopeAnon {
			return scope, id
		}
		return ScopeAnon, "anonymous"
	case ScopeTenant:
		if scope, id := normalizeKey(KeyByTenant()(r)); scope != ScopeAnon {
			return scope, id
		}
		return ScopeAnon, "anonymous"
	case ScopeToken:
		if scope, id := normalizeKey(KeyByTokenHash(16)(r)); scope != ScopeAnon {
			return scope, id
		}
		return ScopeAnon, "anonymous"
	case ScopeAuto, "":
		if scope, id := normalizeKey(KeyByUserOrTenantOrTokenHash(16)(r)); scope != ScopeAnon {
			return scope, id
		}
		return ScopeAnon, "anonymous"
	default:
		return ScopeAnon, "anonymous"
	}
}

// Keyer extracts custom scope and identifier from a request.
type Keyer func(r *http.Request) (Scope, string)

// KeyByAnonymous groups all requests under one anonymous bucket.
func KeyByAnonymous() Keyer {
	return func(r *http.Request) (Scope, string) {
		return ScopeAnon, "anonymous"
	}
}

// KeyByIP resolves identity from trusted client IP information.
func KeyByIP() Keyer {
	return func(r *http.Request) (Scope, string) {
		if r == nil {
			return ScopeAnon, "anonymous"
		}
		if ip, ok := netx.ClientIPFromContext(r.Context()); ok {
			return ScopeIP, ip
		}
		host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
		if err != nil {
			host = strings.TrimSpace(r.RemoteAddr)
		}
		if host == "" {
			return ScopeAnon, "anonymous"
		}
		return ScopeIP, host
	}
}

// KeyByUser resolves identity from authenticated user ID.
func KeyByUser() Keyer {
	return func(r *http.Request) (Scope, string) {
		if r == nil {
			return ScopeAnon, "anonymous"
		}
		principal, ok := auth.FromContext(r.Context())
		if !ok || strings.TrimSpace(principal.UserID) == "" {
			return ScopeAnon, "anonymous"
		}
		return ScopeUser, principal.UserID
	}
}

// KeyByTenant resolves identity from authenticated tenant ID.
func KeyByTenant() Keyer {
	return func(r *http.Request) (Scope, string) {
		if r == nil {
			return ScopeAnon, "anonymous"
		}
		principal, ok := auth.FromContext(r.Context())
		if !ok || strings.TrimSpace(principal.TenantID) == "" {
			return ScopeAnon, "anonymous"
		}
		return ScopeTenant, principal.TenantID
	}
}

// KeyByTokenHash resolves identity from bearer token hash prefix.
func KeyByTokenHash(prefixLen int) Keyer {
	if prefixLen <= 0 {
		prefixLen = 16
	}
	return func(r *http.Request) (Scope, string) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			return ScopeAnon, "anonymous"
		}
		hash := sha256.Sum256([]byte(token))
		hexHash := hex.EncodeToString(hash[:])
		effectivePrefix := prefixLen
		if effectivePrefix > len(hexHash) {
			effectivePrefix = len(hexHash)
		}
		return ScopeToken, hexHash[:effectivePrefix]
	}
}

// KeyByUserOrTenantOrTokenHash resolves user, then tenant, then token-hash identity.
func KeyByUserOrTenantOrTokenHash(prefixLen int) Keyer {
	user := KeyByUser()
	tenant := KeyByTenant()
	token := KeyByTokenHash(prefixLen)
	return func(r *http.Request) (Scope, string) {
		if scope, id := user(r); scope != ScopeAnon {
			return scope, id
		}
		if scope, id := tenant(r); scope != ScopeAnon {
			return scope, id
		}
		if scope, id := token(r); scope != ScopeAnon {
			return scope, id
		}
		return ScopeAnon, "anonymous"
	}
}

// RetryAfterSeconds converts retry duration to whole seconds for Retry-After header.
func RetryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
}

func normalizeRoute(route string) string {
	r := strings.TrimSpace(route)
	if r == "" {
		return "unknown"
	}
	r = strings.ReplaceAll(r, " ", "_")
	if len(r) > 240 {
		return r[:240]
	}
	return r
}

func sanitizeIdentifier(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "_", "\n", "_", "\r", "_", "\t", "_")
	trimmed = replacer.Replace(trimmed)
	if len(trimmed) > 128 {
		trimmed = trimmed[:128]
	}
	return trimmed
}

func normalizeKey(scope Scope, id string) (Scope, string) {
	if scope == "" || scope == ScopeAuto {
		scope = ScopeAnon
	}
	id = sanitizeIdentifier(id)
	if id == "" {
		scope = ScopeAnon
		id = "anonymous"
	}
	return scope, id
}

func validateRequest(req Request) error {
	if req.Limit <= 0 {
		return fmt.Errorf("rate limit must be > 0")
	}
	if req.Window <= 0 {
		return fmt.Errorf("rate limit window must be > 0")
	}
	return nil
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		parsed, _ := strconv.ParseInt(n, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(n), 10, 64)
		return parsed
	default:
		return 0
	}
}

var (
	maxIntBound = int64(^uint(0) >> 1)
	minIntBound = -maxIntBound - 1
)

func int64ToIntBounded(n int64) int {
	if n > maxIntBound {
		return int(maxIntBound)
	}
	if n < minIntBound {
		return int(minIntBound)
	}
	return int(n)
}

func bearerToken(header string) (string, bool) {
	if header == "" {
		return "", false
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	if parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
