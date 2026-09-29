// Package notify delivers the out-of-band secrets goAuth issues for password
// reset and email verification.
//
// goAuth returns these secrets to the application and never sends anything
// itself. The auth module hands them to a Notifier; the HTTP response never
// contains them.
//
// Shipped implementations:
//   - Noop (default, NOTIFY_DRIVER=noop): discards messages.
//   - Log (NOTIFY_DRIVER=log): development logger. Secrets are redacted unless
//     APP_ENV=dev and NOTIFY_LOG_SECRETS=true.
//   - SMTP (NOTIFY_DRIVER=smtp): plain-text mail over the standard library's
//     net/smtp, with STARTTLS or implicit TLS.
//
// To deliver through another provider, implement Notifier and register it with
// RegisterDriver from your own package; New and config lint pick it up without
// editing this package. See docs/auth-flows.md.
package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MrEthical07/superapi/internal/core/logx"
)

// Notifier delivers auth secrets to a user out-of-band.
//
// Implementations receive the raw challenge and must treat it as a secret:
// never log it in full outside development and never echo it to HTTP clients.
type Notifier interface {
	// SendPasswordReset delivers a password-reset challenge to the address.
	SendPasswordReset(ctx context.Context, to, challenge string) error
	// SendEmailVerification delivers an email-verification challenge.
	SendEmailVerification(ctx context.Context, to, challenge string) error
}

// Noop discards every message.
type Noop struct{}

// SendPasswordReset discards the message.
func (Noop) SendPasswordReset(context.Context, string, string) error { return nil }

// SendEmailVerification discards the message.
func (Noop) SendEmailVerification(context.Context, string, string) error { return nil }

// Log writes messages to the application logger for local development.
type Log struct {
	log         *logx.Logger
	showSecrets bool
}

// NewLog creates a log notifier. showSecrets prints full challenges and must
// only be true in development (config lint enforces APP_ENV=dev).
func NewLog(log *logx.Logger, showSecrets bool) *Log {
	return &Log{log: log, showSecrets: showSecrets}
}

// SendPasswordReset logs the reset message.
func (l *Log) SendPasswordReset(_ context.Context, to, challenge string) error {
	l.write("password_reset", to, challenge)
	return nil
}

// SendEmailVerification logs the verification message.
func (l *Log) SendEmailVerification(_ context.Context, to, challenge string) error {
	l.write("email_verification", to, challenge)
	return nil
}

func (l *Log) write(kind, to, challenge string) {
	if l == nil || l.log == nil {
		return
	}
	secret := RedactSecret(challenge)
	if l.showSecrets {
		secret = challenge
	}
	l.log.Info().
		Str("notification", kind).
		Str("to", RedactAddress(to)).
		Str("challenge", secret).
		Msg("notify: message (log driver; not delivered)")
}

// RedactSecret hides a secret while keeping its length visible for debugging.
func RedactSecret(secret string) string {
	return fmt.Sprintf("[redacted len=%d]", len(secret))
}

// RedactAddress keeps the first character of the local part and the domain:
// "alice@example.com" -> "a***@example.com".
func RedactAddress(addr string) string {
	local, domain, ok := strings.Cut(strings.TrimSpace(addr), "@")
	if !ok || local == "" {
		return "***"
	}
	return local[:1] + "***@" + domain
}

// Dispatcher delivers notifications asynchronously so the HTTP response time
// does not depend on whether a message was sent (which would reveal whether
// an account exists) or on the delivery backend's latency.
//
// Concurrency is bounded; when saturated, messages are dropped and logged
// rather than queued without limit. Shutdown drains what is in flight when the
// process stops.
type Dispatcher struct {
	next    Notifier
	log     *logx.Logger
	timeout time.Duration
	slots   chan struct{}

	// mu makes "closed" and "start a delivery" one step, so nothing can begin
	// after Shutdown has started waiting.
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
	// inflight counts running deliveries, for Shutdown's abandoned count.
	inflight atomic.Int64
	// stop is cancelled by Shutdown to tell deliveries still running at the
	// deadline to give up.
	stopCtx context.Context
	stop    context.CancelFunc
}

// NewDispatcher wraps next with bounded asynchronous delivery.
func NewDispatcher(next Notifier, log *logx.Logger, timeout time.Duration, maxInFlight int) *Dispatcher {
	if next == nil {
		next = Noop{}
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if maxInFlight <= 0 {
		maxInFlight = 64
	}
	stopCtx, stop := context.WithCancel(context.Background())
	return &Dispatcher{
		next:    next,
		log:     log,
		timeout: timeout,
		slots:   make(chan struct{}, maxInFlight),
		stopCtx: stopCtx,
		stop:    stop,
	}
}

// PasswordReset schedules delivery of a password-reset challenge.
func (d *Dispatcher) PasswordReset(ctx context.Context, to, challenge string) {
	d.dispatch(ctx, "password_reset", func(sendCtx context.Context) error {
		return d.next.SendPasswordReset(sendCtx, to, challenge)
	})
}

// EmailVerification schedules delivery of an email-verification challenge.
func (d *Dispatcher) EmailVerification(ctx context.Context, to, challenge string) {
	d.dispatch(ctx, "email_verification", func(sendCtx context.Context) error {
		return d.next.SendEmailVerification(sendCtx, to, challenge)
	})
}

func (d *Dispatcher) dispatch(ctx context.Context, kind string, send func(context.Context) error) {
	if d == nil {
		return
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		d.warn(kind, "notify: dispatcher is shut down; message dropped")
		return
	}
	select {
	case d.slots <- struct{}{}:
	default:
		d.mu.Unlock()
		d.warn(kind, "notify: dispatcher saturated; message dropped")
		return
	}
	d.wg.Add(1)
	d.inflight.Add(1)
	d.mu.Unlock()

	// Detach from the request so delivery survives the response, but keep
	// request-scoped values (request id, tracing) for the notifier's logs.
	base := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			<-d.slots
			d.inflight.Add(-1)
			d.wg.Done()
		}()
		sendCtx, cancel := context.WithTimeout(base, d.timeout)
		defer cancel()
		// Shutdown cancels stopCtx if this delivery is still running at the
		// deadline, so a notifier that honours its context exits promptly.
		release := context.AfterFunc(d.stopCtx, cancel)
		defer release()
		if err := send(sendCtx); err != nil && d.log != nil {
			d.log.Error().Err(err).Str("notification", kind).Msg("notify: delivery failed")
		}
	}()
}

func (d *Dispatcher) warn(kind, msg string) {
	if d.log != nil {
		d.log.Warn().Str("notification", kind).Msg(msg)
	}
}

// Shutdown stops accepting new messages, then waits for deliveries already in
// flight until ctx is done. It returns how many had not finished by then (they
// are abandoned and told to stop through their context).
//
// Call it after the HTTP server has stopped, so no handler can still be
// producing messages, and before closing the resources a notifier may use. It
// is safe to call more than once.
func (d *Dispatcher) Shutdown(ctx context.Context) (abandoned int) {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return 0
	case <-ctx.Done():
	}

	// Out of time. Whatever is still running is abandoned: count it, then ask
	// it to stop. A notifier that ignores its context keeps its goroutine
	// until its own timeout, but nothing here waits for it.
	abandoned = int(d.inflight.Load())
	d.stop()
	return abandoned
}

// Wait blocks until in-flight deliveries finish or ctx is done. Intended for
// tests and graceful shutdown.
func (d *Dispatcher) Wait(ctx context.Context) error {
	if d == nil {
		return nil
	}
	for i := 0; i < cap(d.slots); i++ {
		select {
		case d.slots <- struct{}{}:
		case <-ctx.Done():
			for ; i > 0; i-- {
				<-d.slots
			}
			return ctx.Err()
		}
	}
	for i := 0; i < cap(d.slots); i++ {
		<-d.slots
	}
	return nil
}
