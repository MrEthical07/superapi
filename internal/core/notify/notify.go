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
//
// To deliver real email or SMS, implement Notifier (for example over SMTP or a
// provider SDK) and return it from New. See docs/auth-flows.md.
package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
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

// New builds the configured notifier.
func New(cfg config.NotifyConfig, env string, log *logx.Logger) (Notifier, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "", config.NotifyDriverNoop:
		return Noop{}, nil
	case config.NotifyDriverLog:
		show := cfg.LogSecrets && strings.EqualFold(strings.TrimSpace(env), "dev")
		return NewLog(log, show), nil
	default:
		return nil, fmt.Errorf("unknown notify driver %q", cfg.Driver)
	}
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
// rather than queued without limit.
type Dispatcher struct {
	next    Notifier
	log     *logx.Logger
	timeout time.Duration
	slots   chan struct{}
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
	return &Dispatcher{next: next, log: log, timeout: timeout, slots: make(chan struct{}, maxInFlight)}
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
	select {
	case d.slots <- struct{}{}:
	default:
		if d.log != nil {
			d.log.Warn().Str("notification", kind).Msg("notify: dispatcher saturated; message dropped")
		}
		return
	}

	// Detach from the request so delivery survives the response, but keep
	// request-scoped values (request id, tracing) for the notifier's logs.
	base := context.WithoutCancel(ctx)
	go func() {
		defer func() { <-d.slots }()
		sendCtx, cancel := context.WithTimeout(base, d.timeout)
		defer cancel()
		if err := send(sendCtx); err != nil && d.log != nil {
			d.log.Error().Err(err).Str("notification", kind).Msg("notify: delivery failed")
		}
	}()
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
