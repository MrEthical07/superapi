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
)

type Scope string

const (
	ScopeAuto   Scope = "auto"
	ScopeAnon   Scope = "anon"
	ScopeIP     Scope = "ip"
	ScopeUser   Scope = "user"
	ScopeTenant Scope = "tenant"
	ScopeToken  Scope = "token"
)

type Outcome string

const (
	OutcomeAllowed  Outcome = "allowed"
	OutcomeBlocked  Outcome = "blocked"
	OutcomeFailOpen Outcome = "fail_open"
	OutcomeError    Outcome = "error"
)

type Rule struct {
	Limit  int
	Window time.Duration
	Scope  Scope
	Keyer  Keyer
}

func (r Rule) Validate() error {
	if r.Limit <= 0 {
		return fmt.Errorf("rate limit must be > 0")
	}
	if r.Window <= 0 {
		return fmt.Errorf("rate limit window must be > 0")
	}
	return nil
}

type Request struct {
	Route      string
	Scope      Scope
	Identifier string
	Limit      int
	Window     time.Duration
}

type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
	Outcome    Outcome
}

type Limiter interface {
	Allow(ctx context.Context, req Request) (Decision, error)
}

type ObserveFunc func(route string, outcome Outcome)

type Config struct {
	Env      string
	FailOpen bool
	Observe  ObserveFunc
}

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
	current := int(toInt64(values[1]))
	ttl := toInt64(values[2])

	remaining := req.Limit - current
	if remaining < 0 {
		remaining = 0
	}

	decision := Decision{
		Allowed:   allowed,
		Remaining: remaining,
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

type Keyer func(r *http.Request) (Scope, string)

func KeyByAnonymous() Keyer {
	return func(r *http.Request) (Scope, string) {
		return ScopeAnon, "anonymous"
	}
}

func KeyByIP() Keyer {
	return func(r *http.Request) (Scope, string) {
		if r == nil {
			return ScopeAnon, "anonymous"
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
		if prefixLen > len(hexHash) {
			prefixLen = len(hexHash)
		}
		return ScopeToken, hexHash[:prefixLen]
	}
}

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
