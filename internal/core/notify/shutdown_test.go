package notify

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/logx"
)

// slowNotifier takes delay to deliver, honouring its context.
type slowNotifier struct {
	delay     time.Duration
	started   atomic.Int32
	completed atomic.Int32
	cancelled atomic.Int32
}

func (s *slowNotifier) deliver(ctx context.Context) error {
	s.started.Add(1)
	select {
	case <-time.After(s.delay):
		s.completed.Add(1)
		return nil
	case <-ctx.Done():
		s.cancelled.Add(1)
		return ctx.Err()
	}
}

func (s *slowNotifier) SendPasswordReset(ctx context.Context, _, _ string) error {
	return s.deliver(ctx)
}

func (s *slowNotifier) SendEmailVerification(ctx context.Context, _, _ string) error {
	return s.deliver(ctx)
}

// stubbornNotifier ignores its context and blocks until released, like a
// client library that does not support cancellation.
type stubbornNotifier struct {
	release chan struct{}
	started atomic.Int32
}

func (s *stubbornNotifier) SendPasswordReset(context.Context, string, string) error {
	s.started.Add(1)
	<-s.release
	return nil
}

func (s *stubbornNotifier) SendEmailVerification(ctx context.Context, to, c string) error {
	return s.SendPasswordReset(ctx, to, c)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestShutdownWaitsForSlowDeliveries(t *testing.T) {
	n := &slowNotifier{delay: 200 * time.Millisecond}
	d := NewDispatcher(n, nil, 5*time.Second, 8)

	for i := 0; i < 3; i++ {
		d.PasswordReset(context.Background(), "a@example.com", "c")
	}
	waitFor(t, func() bool { return n.started.Load() == 3 })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if abandoned := d.Shutdown(ctx); abandoned != 0 {
		t.Fatalf("abandoned = %d, want 0", abandoned)
	}
	if n.completed.Load() != 3 {
		t.Fatalf("completed = %d, want all 3 delivered before Shutdown returned", n.completed.Load())
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("Shutdown returned in %v without waiting for the slow deliveries", elapsed)
	}
}

func TestShutdownStopsAcceptingNewMessages(t *testing.T) {
	n := &slowNotifier{delay: time.Millisecond}
	buf := &syncBuffer{}
	log, err := logx.NewWithWriter(logx.Config{Level: "debug", Format: "json"}, buf)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	d := NewDispatcher(n, log, time.Second, 8)

	if abandoned := d.Shutdown(context.Background()); abandoned != 0 {
		t.Fatalf("abandoned = %d", abandoned)
	}
	d.PasswordReset(context.Background(), "a@example.com", "c")
	d.EmailVerification(context.Background(), "a@example.com", "c")
	time.Sleep(50 * time.Millisecond)

	if n.started.Load() != 0 {
		t.Fatalf("a message dispatched after Shutdown must not be delivered (started=%d)", n.started.Load())
	}
	if got := strings.Count(buf.String(), "dispatcher is shut down; message dropped"); got != 2 {
		t.Fatalf("expected 2 drop warnings, got %d: %s", got, buf.String())
	}
}

func TestShutdownAbandonsHungDeliveriesAtTheDeadline(t *testing.T) {
	n := &slowNotifier{delay: time.Hour} // honours its context
	d := NewDispatcher(n, nil, time.Hour, 8)

	d.PasswordReset(context.Background(), "a@example.com", "c")
	d.EmailVerification(context.Background(), "b@example.com", "c")
	waitFor(t, func() bool { return n.started.Load() == 2 })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	abandoned := d.Shutdown(ctx)
	elapsed := time.Since(start)

	if abandoned != 2 {
		t.Fatalf("abandoned = %d, want 2", abandoned)
	}
	if elapsed < 100*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("Shutdown took %v, want about the 150ms deadline", elapsed)
	}
	// The abandoned deliveries were told to stop.
	waitFor(t, func() bool { return n.cancelled.Load() == 2 })
	if n.completed.Load() != 0 {
		t.Fatal("a hung delivery must not be reported as completed")
	}
}

// A notifier that ignores its context cannot block shutdown either.
func TestShutdownDoesNotBlockOnANotifierThatIgnoresItsContext(t *testing.T) {
	n := &stubbornNotifier{release: make(chan struct{})}
	t.Cleanup(func() { close(n.release) })
	d := NewDispatcher(n, nil, time.Hour, 4)

	d.PasswordReset(context.Background(), "a@example.com", "c")
	waitFor(t, func() bool { return n.started.Load() == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	abandoned := d.Shutdown(ctx)
	if abandoned != 1 {
		t.Fatalf("abandoned = %d, want 1", abandoned)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Shutdown blocked for %v on a stubborn notifier", elapsed)
	}
}

func TestShutdownIsIdempotentAndSafeOnNil(t *testing.T) {
	var nilDispatcher *Dispatcher
	if nilDispatcher.Shutdown(context.Background()) != 0 {
		t.Fatal("nil dispatcher must be a no-op")
	}

	d := NewDispatcher(&slowNotifier{delay: time.Millisecond}, nil, time.Second, 2)
	for i := 0; i < 3; i++ {
		if d.Shutdown(context.Background()) != 0 {
			t.Fatalf("call %d: nothing was in flight", i)
		}
	}
}

// Dispatches racing with Shutdown must be either fully delivered or dropped,
// never lost half-way, and never panic (run with -race).
func TestShutdownRacesWithDispatch(t *testing.T) {
	n := &slowNotifier{delay: 5 * time.Millisecond}
	d := NewDispatcher(n, nil, 5*time.Second, 1024)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					d.PasswordReset(context.Background(), "a@example.com", "c")
				}
			}
		}()
	}
	time.Sleep(30 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if abandoned := d.Shutdown(ctx); abandoned != 0 {
		t.Fatalf("abandoned = %d, want 0", abandoned)
	}
	close(stop)
	wg.Wait()

	if n.started.Load() != n.completed.Load() {
		t.Fatalf("started=%d completed=%d: a delivery was cut off", n.started.Load(), n.completed.Load())
	}
	// Nothing may start after Shutdown returned.
	before := n.started.Load()
	d.PasswordReset(context.Background(), "a@example.com", "c")
	time.Sleep(30 * time.Millisecond)
	if n.started.Load() != before {
		t.Fatal("a delivery started after Shutdown")
	}
}
