package app

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
	"github.com/MrEthical07/superapi/internal/core/notify"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeNotifier records how each delivery ended.
type fakeNotifier struct {
	delay     time.Duration // how long a delivery takes
	ignoreCtx bool          // a stubborn client library that never returns
	release   chan struct{} // unblocks stubborn deliveries at test end

	started   atomic.Int32
	completed atomic.Int32
	cancelled atomic.Int32
}

func (f *fakeNotifier) deliver(ctx context.Context) error {
	f.started.Add(1)
	if f.ignoreCtx {
		<-f.release
		return nil
	}
	select {
	case <-time.After(f.delay):
		f.completed.Add(1)
		return nil
	case <-ctx.Done():
		f.cancelled.Add(1)
		return ctx.Err()
	}
}

func (f *fakeNotifier) SendPasswordReset(ctx context.Context, _, _ string) error {
	return f.deliver(ctx)
}

func (f *fakeNotifier) SendEmailVerification(ctx context.Context, _, _ string) error {
	return f.deliver(ctx)
}

func newShutdownApp(t *testing.T, n notify.Notifier, timeout time.Duration) (*App, *notify.Dispatcher, *syncBuffer) {
	t.Helper()
	buf := &syncBuffer{}
	log, err := logx.NewWithWriter(logx.Config{Level: "debug", Format: "json"}, buf)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	cfg := &config.Config{ServiceName: "svc", Env: "test"}
	cfg.HTTP.Addr = "127.0.0.1:0"
	cfg.HTTP.ShutdownTimeout = timeout

	dispatcher := notify.NewDispatcher(n, log, time.Hour, 16)
	a := &App{
		cfg:    cfg,
		log:    log,
		server: &http.Server{Addr: cfg.HTTP.Addr, Handler: http.NotFoundHandler()},
		deps:   &Dependencies{Notifier: dispatcher},
	}
	return a, dispatcher, buf
}

// runUntilShutdown starts the app, waits for the listener, triggers shutdown
// and returns how long Run took to return after it.
func runUntilShutdown(t *testing.T, a *App) (time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(100 * time.Millisecond) // let ListenAndServe start
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		return time.Since(start), err
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return")
		return 0, nil
	}
}

// A slow notifier finishes during graceful shutdown: Run does not return, and
// dependencies do not close, until the message is out.
func TestGracefulShutdownCompletesSlowDeliveries(t *testing.T) {
	n := &fakeNotifier{delay: 400 * time.Millisecond}
	a, dispatcher, logs := newShutdownApp(t, n, 5*time.Second)

	dispatcher.PasswordReset(context.Background(), "alice@example.com", "challenge")
	dispatcher.EmailVerification(context.Background(), "bob@example.com", "challenge")

	took, err := runUntilShutdown(t, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n.completed.Load() != 2 {
		t.Fatalf("completed = %d, want both deliveries finished before Run returned", n.completed.Load())
	}
	if n.cancelled.Load() != 0 {
		t.Fatal("no delivery may be cancelled when it fits inside the shutdown timeout")
	}
	if took >= 4*time.Second {
		t.Fatalf("Run took %v; it should return as soon as deliveries finish", took)
	}
	if strings.Contains(logs.String(), "abandoned") {
		t.Fatalf("nothing was abandoned, but the log says so: %s", logs.String())
	}

	// After shutdown the dispatcher accepts nothing.
	dispatcher.PasswordReset(context.Background(), "late@example.com", "challenge")
	time.Sleep(50 * time.Millisecond)
	if n.started.Load() != 2 {
		t.Fatalf("a message dispatched after shutdown was delivered (started=%d)", n.started.Load())
	}
}

// A hung notifier is abandoned when the shutdown timeout runs out, is told to
// stop, and Run still returns; the abandoned count is logged.
func TestGracefulShutdownAbandonsHungDeliveryAtTheDeadline(t *testing.T) {
	n := &fakeNotifier{delay: time.Hour}
	a, dispatcher, logs := newShutdownApp(t, n, 600*time.Millisecond)

	dispatcher.PasswordReset(context.Background(), "alice@example.com", "challenge")
	for n.started.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	took, err := runUntilShutdown(t, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if took < 400*time.Millisecond || took > 5*time.Second {
		t.Fatalf("Run took %v, want about the 600ms shutdown timeout", took)
	}
	out := logs.String()
	if !strings.Contains(out, "abandoned in-flight notification deliveries") || !strings.Contains(out, `"abandoned":1`) {
		t.Fatalf("expected the abandoned count in the log: %s", out)
	}
	if strings.Contains(out, "alice@example.com") || strings.Contains(out, "challenge") {
		t.Fatalf("log leaks the address or challenge: %s", out)
	}
	deadline := time.Now().Add(3 * time.Second)
	for n.cancelled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n.cancelled.Load() != 1 {
		t.Fatal("the abandoned delivery must be told to stop through its context")
	}
}

// Even a notifier that never returns and ignores its context cannot keep the
// process from exiting.
func TestGracefulShutdownIsNotBlockedByAStubbornNotifier(t *testing.T) {
	n := &fakeNotifier{ignoreCtx: true, release: make(chan struct{})}
	t.Cleanup(func() { close(n.release) })
	a, dispatcher, logs := newShutdownApp(t, n, 500*time.Millisecond)

	dispatcher.PasswordReset(context.Background(), "alice@example.com", "challenge")
	for n.started.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	took, err := runUntilShutdown(t, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if took > 5*time.Second {
		t.Fatalf("Run took %v; a stubborn notifier blocked exit", took)
	}
	if !strings.Contains(logs.String(), `"abandoned":1`) {
		t.Fatalf("expected the abandoned count in the log: %s", logs.String())
	}
}

// Shutdown waits at most once: closeDependencies after Run's own drain must
// not add a second wait for the same stuck delivery.
func TestNotifierIsDrainedOnlyOnce(t *testing.T) {
	n := &fakeNotifier{delay: time.Hour}
	a, dispatcher, _ := newShutdownApp(t, n, 300*time.Millisecond)
	dispatcher.PasswordReset(context.Background(), "alice@example.com", "challenge")
	for n.started.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	took, err := runUntilShutdown(t, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if took > 2*time.Second {
		t.Fatalf("Run took %v, want a single 300ms wait", took)
	}

	start := time.Now()
	a.closeDependencies()
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("a second closeDependencies waited %v", elapsed)
	}
}

// On exit paths that never began a graceful shutdown (for example a listener
// that failed to start), closeDependencies still drains the notifier, bounded
// by the shutdown timeout.
func TestCloseDependenciesDrainsWhenShutdownNeverBegan(t *testing.T) {
	n := &fakeNotifier{delay: 200 * time.Millisecond}
	a, dispatcher, _ := newShutdownApp(t, n, 5*time.Second)
	dispatcher.PasswordReset(context.Background(), "alice@example.com", "challenge")
	for n.started.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	a.closeDependencies()
	if n.completed.Load() != 1 {
		t.Fatalf("completed = %d, want the pending delivery finished", n.completed.Load())
	}
}
