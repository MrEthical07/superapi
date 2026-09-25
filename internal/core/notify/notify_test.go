package notify

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

func newBufferLogger(t *testing.T) (*logx.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log, err := logx.NewWithWriter(logx.Config{Level: "debug", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	return log, &buf
}

func TestLogNotifierRedactsByDefault(t *testing.T) {
	log, buf := newBufferLogger(t)
	n, err := New(config.NotifyConfig{Driver: "log"}, "dev", log)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	const secret = "tenant:verification-id:super-secret-code"
	_ = n.SendPasswordReset(context.Background(), "alice@example.com", secret)
	out := buf.String()
	if strings.Contains(out, "super-secret-code") || strings.Contains(out, "alice@") {
		t.Fatalf("secret or address leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted len=") || !strings.Contains(out, "a***@example.com") {
		t.Fatalf("expected redacted output: %s", out)
	}
}

func TestLogNotifierShowsSecretsOnlyInDev(t *testing.T) {
	for _, tc := range []struct {
		env  string
		show bool
	}{{"dev", true}, {"prod", false}, {"staging", false}} {
		log, buf := newBufferLogger(t)
		n, err := New(config.NotifyConfig{Driver: "log", LogSecrets: true}, tc.env, log)
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		_ = n.SendEmailVerification(context.Background(), "bob@example.com", "the-secret")
		if got := strings.Contains(buf.String(), "the-secret"); got != tc.show {
			t.Fatalf("env=%s secret shown=%v want %v: %s", tc.env, got, tc.show, buf.String())
		}
	}
}

func TestNewNotifierDrivers(t *testing.T) {
	if n, err := New(config.NotifyConfig{}, "dev", nil); err != nil {
		t.Fatalf("default: %v", err)
	} else if _, ok := n.(Noop); !ok {
		t.Fatalf("default driver = %T, want Noop", n)
	}
	if _, err := New(config.NotifyConfig{Driver: "smtp"}, "dev", nil); err == nil {
		t.Fatal("unknown driver must error")
	}
}

type countingNotifier struct {
	calls   atomic.Int32
	release chan struct{}
}

func (c *countingNotifier) SendPasswordReset(ctx context.Context, _, _ string) error {
	c.calls.Add(1)
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
		}
	}
	return nil
}

func (c *countingNotifier) SendEmailVerification(ctx context.Context, to, ch string) error {
	return c.SendPasswordReset(ctx, to, ch)
}

func TestDispatcherIsAsyncAndBounded(t *testing.T) {
	inner := &countingNotifier{release: make(chan struct{})}
	d := NewDispatcher(inner, nil, time.Second, 2)

	start := time.Now()
	for i := 0; i < 5; i++ {
		d.PasswordReset(context.Background(), "a@example.com", "c")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("dispatch must not block on delivery")
	}
	close(inner.release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.Wait(ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Fatalf("delivered %d, want 2 (bounded, rest dropped)", got)
	}
}

func TestDispatcherSurvivesCanceledRequest(t *testing.T) {
	inner := &countingNotifier{}
	d := NewDispatcher(inner, nil, time.Second, 4)
	ctx, cancel := context.WithCancel(context.Background())
	d.EmailVerification(ctx, "a@example.com", "c")
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if err := d.Wait(waitCtx); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Fatal("delivery must survive the request context being canceled")
	}
}

func TestRedactAddress(t *testing.T) {
	cases := map[string]string{"alice@example.com": "a***@example.com", "no-at": "***", "@x": "***"}
	for in, want := range cases {
		if got := RedactAddress(in); got != want {
			t.Errorf("RedactAddress(%q)=%q want %q", in, got, want)
		}
	}
}
