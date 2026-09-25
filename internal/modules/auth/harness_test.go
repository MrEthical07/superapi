package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/app"
	coreauth "github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/notify"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/tenant"
)

const testPassword = "correct-horse-battery-staple"

// captureNotifier records every message the module asks to deliver.
type captureNotifier struct {
	mu       sync.Mutex
	resets   map[string]string
	verifies map[string]string
}

func newCaptureNotifier() *captureNotifier {
	return &captureNotifier{resets: map[string]string{}, verifies: map[string]string{}}
}

func (c *captureNotifier) SendPasswordReset(_ context.Context, to, challenge string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resets[to] = challenge
	return nil
}

func (c *captureNotifier) SendEmailVerification(_ context.Context, to, challenge string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.verifies[to] = challenge
	return nil
}

func (c *captureNotifier) reset(to string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.resets[to]
	return v, ok
}

func (c *captureNotifier) verification(to string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.verifies[to]
	return v, ok
}

type harnessOptions struct {
	tenancy bool
	auth    config.AuthConfig
}

type harness struct {
	t          *testing.T
	engine     *goauth.Engine
	handler    http.Handler
	notifier   *captureNotifier
	dispatcher *notify.Dispatcher
	users      *authtest.UserRepository
}

// newHarness wires the real auth module, goAuth engine, provider and (when
// tenancy is on) tenant middleware over in-memory repositories.
func newHarness(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	if opts.tenancy {
		prev := policy.TenancyEnabled()
		policy.SetTenancyEnabled(true)
		t.Cleanup(func() { policy.SetTenancyEnabled(prev) })
	}

	opts.auth.Enabled = true
	users := authtest.NewUserRepository()
	engine, _ := authtest.NewEngineWithFeatures(t, opts.tenancy, users, coreauth.Features{
		RegistrationAutoLogin:     opts.auth.RegistrationEnabled && opts.auth.RegistrationAutoLogin,
		PasswordReset:             opts.auth.PasswordResetEnabled,
		EmailVerification:         opts.auth.EmailVerificationEnabled,
		EmailVerificationRequired: opts.auth.EmailVerificationRequired,
		TOTP:                      opts.auth.TOTPEnabled,
		TOTPIssuer:                "SuperAPI Test",
	})

	capture := newCaptureNotifier()
	dispatcher := notify.NewDispatcher(capture, nil, time.Second, 8)

	m := New()
	m.BindDependencies(&app.Dependencies{
		AuthEngine: engine,
		AuthMode:   coreauth.ModeStrict,
		Auth:       opts.auth,
		AuthUsers:  users,
		Notifier:   dispatcher,
	})
	mux := httpx.NewMux()
	if err := m.Register(mux); err != nil {
		t.Fatalf("register: %v", err)
	}
	var handler http.Handler = mux
	if opts.tenancy {
		handler = tenant.Middleware(tenant.ResolverConfig{Header: "X-Tenant-ID"})(mux)
	}
	return &harness{t: t, engine: engine, handler: handler, notifier: capture, dispatcher: dispatcher, users: users}
}

type call struct {
	method string
	path   string
	body   any
	token  string
	tenant string
}

type result struct {
	status int
	body   []byte
}

// data decodes the envelope's data field into v.
func (r result) data(t *testing.T, v any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode envelope %s: %v", r.body, err)
	}
	if err := json.Unmarshal(env.Data, v); err != nil {
		t.Fatalf("decode data %s: %v", env.Data, err)
	}
}

// comparable strips the per-request request_id so bodies can be compared.
func (r result) comparable(t *testing.T) string {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode envelope %s: %v", r.body, err)
	}
	delete(env, "request_id")
	out, _ := json.Marshal(env)
	return string(out)
}

func (h *harness) do(c call) result {
	h.t.Helper()
	var buf bytes.Buffer
	if c.body != nil {
		if err := json.NewEncoder(&buf).Encode(c.body); err != nil {
			h.t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(c.method, c.path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.tenant != "" {
		req.Header.Set("X-Tenant-ID", c.tenant)
	}
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	return result{status: rr.Code, body: rr.Body.Bytes()}
}

func (h *harness) post(path string, body any) result {
	return h.do(call{method: http.MethodPost, path: path, body: body})
}

// flush waits for asynchronous notifications.
func (h *harness) flush() {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.dispatcher.Wait(ctx); err != nil {
		h.t.Fatalf("wait for notifications: %v", err)
	}
}

// createUser creates an active account directly through goAuth.
func (h *harness) createUser(identifier string) string {
	h.t.Helper()
	res, err := h.engine.CreateAccount(context.Background(), goauth.CreateAccountRequest{Identifier: identifier, Password: testPassword})
	if err != nil {
		h.t.Fatalf("create account: %v", err)
	}
	return res.UserID
}

// login returns the access token, failing the test otherwise.
func (h *harness) login(identifier, password string) string {
	h.t.Helper()
	res := h.post("/api/v1/auth/login", map[string]string{"identifier": identifier, "password": password})
	if res.status != http.StatusOK {
		h.t.Fatalf("login %s: status=%d body=%s", identifier, res.status, res.body)
	}
	var tok tokenResponse
	res.data(h.t, &tok)
	if tok.AccessToken == "" {
		h.t.Fatalf("login %s returned no token: %s", identifier, res.body)
	}
	return tok.AccessToken
}

// totpCode computes an RFC 6238 code (SHA1, 6 digits, 30s) for the time step
// offset steps away from the current one. goAuth accepts a skew of one step.
func totpCode(t *testing.T, secretBase32 string, offset int64) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secretBase32, "=")))
	if err != nil {
		t.Fatalf("decode totp secret: %v", err)
	}
	counter := time.Now().Unix()/30 + offset
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}
