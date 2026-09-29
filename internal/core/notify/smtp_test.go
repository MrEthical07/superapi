package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

const (
	testResetURL  = "https://app.example.com/reset?token={token}"
	testVerifyURL = "https://app.example.com/verify/{token}"
)

func smtpConfig(s *fakeSMTP, mode string) config.SMTPConfig {
	return config.SMTPConfig{
		Host:      s.host(),
		Port:      s.port(),
		From:      "SuperAPI <noreply@example.com>",
		TLS:       mode,
		ResetURL:  testResetURL,
		VerifyURL: testVerifyURL,
	}
}

func newTestSMTP(t *testing.T, cfg config.SMTPConfig) *SMTP {
	t.Helper()
	n, err := NewSMTP(cfg)
	if err != nil {
		t.Fatalf("NewSMTP: %v", err)
	}
	return n
}

func sendCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func parseMessage(t *testing.T, raw string) (*mail.Message, string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("message does not parse: %v\n%s", err, raw)
	}
	var body strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := msg.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return msg, body.String()
}

// The development path: no TLS, no auth, against a plain server.
func TestSMTPPasswordResetInDevModeWithoutTLS(t *testing.T) {
	srv := startFakeSMTP(t, nil)
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSNone))

	const challenge = "tenant:abc/def+ghi jkl&x=1#frag"
	if err := n.SendPasswordReset(sendCtx(t), "alice@example.com", challenge); err != nil {
		t.Fatalf("send: %v", err)
	}

	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("messages = %d, want 1", len(got))
	}
	m := got[0]
	if m.viaTLS {
		t.Fatal("dev mode must not use TLS")
	}
	if m.from != "noreply@example.com" {
		t.Fatalf("envelope sender = %q", m.from)
	}
	if len(m.rcpts) != 1 || m.rcpts[0] != "alice@example.com" {
		t.Fatalf("envelope recipients = %v", m.rcpts)
	}

	msg, body := parseMessage(t, m.data)
	header := func(k string) string { return msg.Header.Get(k) }
	if header("From") != `"SuperAPI" <noreply@example.com>` {
		t.Fatalf("From = %q", header("From"))
	}
	if header("To") != "alice@example.com" {
		t.Fatalf("To = %q", header("To"))
	}
	if header("Subject") != "Reset your password" {
		t.Fatalf("Subject = %q", header("Subject"))
	}
	date, err := msg.Header.Date()
	if err != nil {
		t.Fatalf("Date header does not parse: %v (%q)", err, header("Date"))
	}
	if d := time.Since(date); d < -time.Minute || d > time.Minute {
		t.Fatalf("Date %v is not current", date)
	}
	if !regexp.MustCompile(`^<[0-9a-f]{32}@example\.com>$`).MatchString(header("Message-ID")) {
		t.Fatalf("Message-ID = %q", header("Message-ID"))
	}
	if header("MIME-Version") != "1.0" || header("Content-Type") != "text/plain; charset=UTF-8" || header("Content-Transfer-Encoding") != "7bit" {
		t.Fatalf("MIME headers: %v", msg.Header)
	}

	wantLink := "https://app.example.com/reset?token=tenant%3Aabc%2Fdef%2Bghi%20jkl%26x%3D1%23frag"
	if !strings.Contains(body, wantLink+"\r\n") {
		t.Fatalf("body does not carry the escaped link %q:\n%s", wantLink, body)
	}
	if strings.Contains(body, challenge) {
		t.Fatalf("raw challenge must not appear in the body:\n%s", body)
	}
	if !strings.Contains(body, "reset the password") || !strings.Contains(body, "ignore this message") {
		t.Fatalf("unexpected body text:\n%s", body)
	}
	if strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\n") {
		t.Fatal("body must use CRLF line endings")
	}
}

func TestSMTPEmailVerificationMessage(t *testing.T) {
	srv := startFakeSMTP(t, nil)
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSNone))

	if err := n.SendEmailVerification(sendCtx(t), "bob@example.com", "0f3a-9c"); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	msg, body := parseMessage(t, got[0].data)
	if msg.Header.Get("Subject") != "Verify your email address" {
		t.Fatalf("Subject = %q", msg.Header.Get("Subject"))
	}
	if !strings.Contains(body, "https://app.example.com/verify/0f3a-9c\r\n") {
		t.Fatalf("body = %q", body)
	}
}

func TestBuildLinkEscaping(t *testing.T) {
	tests := []struct {
		name, template, challenge, want string
	}{
		{"plain token", "https://x.test/r?t={token}", "abc123", "https://x.test/r?t=abc123"},
		{"colons and slashes", "https://x.test/r?t={token}", "tenant:id/code", "https://x.test/r?t=tenant%3Aid%2Fcode"},
		{"query delimiters", "https://x.test/r?t={token}", "a&b=c?d#e", "https://x.test/r?t=a%26b%3Dc%3Fd%23e"},
		{"plus and space", "https://x.test/r?t={token}", "a+b c", "https://x.test/r?t=a%2Bb%20c"},
		{"path position", "https://x.test/verify/{token}/done", "a/b c", "https://x.test/verify/a%2Fb%20c/done"},
		{"percent", "https://x.test/r?t={token}", "100%", "https://x.test/r?t=100%25"},
		{"unicode", "https://x.test/r?t={token}", "é", "https://x.test/r?t=%C3%A9"},
		{"injection attempt", "https://x.test/r?t={token}", "x\r\nBcc: evil@example.com", "https://x.test/r?t=x%0D%0ABcc%3A%20evil%40example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildLink(tc.template, tc.challenge)
			if err != nil {
				t.Fatalf("buildLink: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
	if _, err := buildLink("", "abc"); err == nil {
		t.Fatal("empty template must error")
	}
	if _, err := buildLink(testResetURL, ""); err == nil {
		t.Fatal("empty challenge must error")
	}
}

func TestSMTPRejectsHeaderInjection(t *testing.T) {
	srv := startFakeSMTP(t, nil)
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSNone))

	recipients := []string{
		"alice@example.com\r\nBcc: evil@example.com",
		"alice@example.com\nBcc: evil@example.com",
		"alice@example.com\rBcc: evil@example.com",
		"\r\nalice@example.com",
		"alice@example.com, evil@example.com",
		"not-an-address",
		"",
		"alice@example.com\x00evil",
	}
	for _, to := range recipients {
		if err := n.SendPasswordReset(sendCtx(t), to, "tok"); err == nil {
			t.Fatalf("recipient %q must be rejected", to)
		}
		if err := n.SendEmailVerification(sendCtx(t), to, "tok"); err == nil {
			t.Fatalf("recipient %q must be rejected for verification", to)
		}
	}
	if srv.connectionCount() != 0 || len(srv.received()) != 0 {
		t.Fatalf("a rejected recipient must never reach the server (connections=%d messages=%d)", srv.connectionCount(), len(srv.received()))
	}

	// A subject with a line break is rejected by the message builder.
	if _, err := n.buildMessage("alice@example.com", "Hello\r\nBcc: evil@example.com", "body\n"); err == nil {
		t.Fatal("subject with CRLF must be rejected")
	}
	if _, err := n.buildMessage("alice@example.com", "Hello\nBcc: evil@example.com", "body\n"); err == nil {
		t.Fatal("subject with LF must be rejected")
	}
	if _, err := n.buildMessage("alice@example.com\r\nBcc: e@example.com", "Hello", "body\n"); err == nil {
		t.Fatal("recipient with CRLF must be rejected by the message builder")
	}
	if _, err := n.buildMessage("alice@example.com", "Hello", "caf\u00e9\n"); err == nil {
		t.Fatal("non-ASCII body must be rejected (7bit encoding)")
	}
}

// A challenge with line breaks cannot add a header either: it only ever
// appears percent-encoded inside the link.
func TestSMTPChallengeCannotInjectHeaders(t *testing.T) {
	srv := startFakeSMTP(t, nil)
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSNone))

	if err := n.SendPasswordReset(sendCtx(t), "alice@example.com", "x\r\nBcc: evil@example.com\r\n\r\nspoofed"); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	msg, _ := parseMessage(t, got[0].data)
	if msg.Header.Get("Bcc") != "" {
		t.Fatalf("challenge injected a header: %v", msg.Header)
	}
	if len(got[0].rcpts) != 1 {
		t.Fatalf("recipients = %v", got[0].rcpts)
	}
}

func TestSMTPStartTLS(t *testing.T) {
	tlsCfg, pool := selfSignedTLS(t)
	srv := startFakeSMTP(t, func(s *fakeSMTP) { s.tlsConfig = tlsCfg; s.advertiseAuth = true })

	cfg := smtpConfig(srv, config.SMTPTLSStartTLS)
	cfg.Username, cfg.Password = "mailer", "s3cret-pass"
	n := newTestSMTP(t, cfg)
	n.tlsConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}

	if err := n.SendPasswordReset(sendCtx(t), "alice@example.com", "tok"); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := srv.received()
	if len(got) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	if !got[0].viaTLS {
		t.Fatal("message must have been sent after STARTTLS")
	}
	if got[0].authUser != "mailer" || got[0].authPass != "s3cret-pass" {
		t.Fatalf("credentials = %q/%q", got[0].authUser, got[0].authPass)
	}
}

func TestSMTPImplicitTLS(t *testing.T) {
	tlsCfg, pool := selfSignedTLS(t)
	srv := startFakeSMTP(t, func(s *fakeSMTP) { s.tlsConfig = tlsCfg; s.implicit = true })

	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSImplicit))
	n.tlsConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}

	if err := n.SendEmailVerification(sendCtx(t), "alice@example.com", "tok"); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := srv.received()
	if len(got) != 1 || !got[0].viaTLS {
		t.Fatalf("messages = %+v, want one over TLS", got)
	}
}

// STARTTLS mode never downgrades: if the server does not offer it, nothing is
// sent.
func TestSMTPStartTLSRefusesToDowngrade(t *testing.T) {
	srv := startFakeSMTP(t, nil) // no TLS offered
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSStartTLS))

	err := n.SendPasswordReset(sendCtx(t), "alice@example.com", "tok")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want a STARTTLS refusal", err)
	}
	if srv.sawCommand("MAIL FROM") || srv.sawCommand("DATA") || srv.sawCommand("AUTH") {
		t.Fatal("nothing may be sent (and no credentials offered) without encryption")
	}
}

// An untrusted certificate fails the handshake rather than sending anyway.
func TestSMTPStartTLSVerifiesTheCertificate(t *testing.T) {
	tlsCfg, _ := selfSignedTLS(t)
	srv := startFakeSMTP(t, func(s *fakeSMTP) { s.tlsConfig = tlsCfg })

	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSStartTLS)) // default roots: self-signed is untrusted
	if err := n.SendPasswordReset(sendCtx(t), "alice@example.com", "tok"); err == nil {
		t.Fatal("a certificate that does not verify must fail the send")
	}
	if srv.sawCommand("MAIL FROM") {
		t.Fatal("no mail may be sent over an unverified connection")
	}
}

func TestSMTPAuthRequiredButNotOffered(t *testing.T) {
	srv := startFakeSMTP(t, nil)
	cfg := smtpConfig(srv, config.SMTPTLSNone)
	cfg.Username, cfg.Password = "mailer", "pw"
	err := newTestSMTP(t, cfg).SendPasswordReset(sendCtx(t), "alice@example.com", "tok")
	if err == nil || !strings.Contains(err.Error(), "AUTH") {
		t.Fatalf("err = %v, want an AUTH error", err)
	}
}

// Server rejection text often echoes the address; errors must carry only the
// redacted form, and never the challenge or the link.
func TestSMTPErrorsRedactAddressAndNeverCarrySecrets(t *testing.T) {
	srv := startFakeSMTP(t, func(s *fakeSMTP) { s.rejectRcpt = "alice@example.com" })
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSNone))

	const challenge = "super-secret-challenge-value"
	err := n.SendPasswordReset(sendCtx(t), "alice@example.com", challenge)
	if err == nil {
		t.Fatal("expected the server's rejection")
	}
	text := err.Error()
	for _, leak := range []string{"alice@example.com", challenge, "app.example.com"} {
		if strings.Contains(text, leak) {
			t.Fatalf("error leaks %q: %s", leak, text)
		}
	}
	if !strings.Contains(text, "a***@example.com") {
		t.Fatalf("error should carry the redacted address: %s", text)
	}
	var netErr interface{ Timeout() bool }
	_ = errors.As(err, &netErr) // the cause chain stays intact for callers
	if errors.Unwrap(err) == nil {
		t.Fatal("the original error must stay reachable with errors.Unwrap")
	}
}

func TestSMTPConnectionFailureCarriesNoSecrets(t *testing.T) {
	srv := startFakeSMTP(t, nil)
	cfg := smtpConfig(srv, config.SMTPTLSNone)
	_ = srv.ln.Close()
	n := newTestSMTP(t, cfg)

	err := n.SendPasswordReset(sendCtx(t), "alice@example.com", "very-secret-challenge")
	if err == nil {
		t.Fatal("expected a connection error")
	}
	for _, leak := range []string{"very-secret-challenge", "alice@example.com", "app.example.com"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("error leaks %q: %s", leak, err)
		}
	}
}

// A server that hangs after the greeting is abandoned at the delivery
// deadline instead of blocking forever.
func TestSMTPHonoursTheDeliveryDeadline(t *testing.T) {
	srv := startFakeSMTP(t, func(s *fakeSMTP) { s.stallAfterHi = 5 * time.Second })
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSNone))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := n.SendPasswordReset(ctx, "alice@example.com", "tok")
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("send took %v; the deadline was not honoured", elapsed)
	}
}

func TestNewSMTPValidatesConfig(t *testing.T) {
	good := config.SMTPConfig{Host: "smtp.example.com", Port: 587, From: "noreply@example.com", TLS: config.SMTPTLSStartTLS, ResetURL: testResetURL}
	if _, err := NewSMTP(good); err != nil {
		t.Fatalf("good config: %v", err)
	}
	mutations := map[string]func(*config.SMTPConfig){
		"no host":       func(c *config.SMTPConfig) { c.Host = "" },
		"bad port":      func(c *config.SMTPConfig) { c.Port = 0 },
		"bad tls":       func(c *config.SMTPConfig) { c.TLS = "ssl" },
		"bad from":      func(c *config.SMTPConfig) { c.From = "not an address" },
		"from injected": func(c *config.SMTPConfig) { c.From = "a@example.com\r\nBcc: x@example.com" },
		"template without token": func(c *config.SMTPConfig) {
			c.ResetURL = "https://app.example.com/reset"
		},
		"template with two tokens": func(c *config.SMTPConfig) { c.ResetURL = "https://a.test/{token}/{token}" },
		"relative template":        func(c *config.SMTPConfig) { c.ResetURL = "/reset?token={token}" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			cfg := good
			mutate(&cfg)
			if _, err := NewSMTP(cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSMTPMissingLinkTemplate(t *testing.T) {
	n := newTestSMTP(t, config.SMTPConfig{Host: "127.0.0.1", Port: 1, From: "a@example.com", TLS: config.SMTPTLSNone})
	if err := n.SendPasswordReset(sendCtx(t), "alice@example.com", "tok"); err == nil || !strings.Contains(err.Error(), "NOTIFY_RESET_URL") {
		t.Fatalf("err = %v, want a hint about NOTIFY_RESET_URL", err)
	}
	if err := n.SendEmailVerification(sendCtx(t), "alice@example.com", "tok"); err == nil || !strings.Contains(err.Error(), "NOTIFY_VERIFY_URL") {
		t.Fatalf("err = %v, want a hint about NOTIFY_VERIFY_URL", err)
	}
}

// syncBuffer is a bytes.Buffer safe for the dispatcher's concurrent writers.
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

// Through the real dispatcher, a failed delivery is logged without the
// challenge, the link, or the full address.
func TestSMTPFailureLogsNothingSecret(t *testing.T) {
	srv := startFakeSMTP(t, func(s *fakeSMTP) { s.rejectRcpt = "alice@example.com" })
	n := newTestSMTP(t, smtpConfig(srv, config.SMTPTLSNone))

	buf := &syncBuffer{}
	log, err := logx.NewWithWriter(logx.Config{Level: "debug", Format: "json"}, buf)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	d := NewDispatcher(n, log, 5*time.Second, 4)
	d.PasswordReset(context.Background(), "alice@example.com", "log-secret-challenge")
	d.EmailVerification(context.Background(), "alice@example.com", "log-secret-verification")
	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.Wait(waitCtx); err != nil {
		t.Fatalf("wait: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "delivery failed") {
		t.Fatalf("expected a delivery failure to be logged: %q", out)
	}
	for _, leak := range []string{"log-secret-challenge", "log-secret-verification", "alice@example.com", "app.example.com"} {
		if strings.Contains(out, leak) {
			t.Fatalf("log leaks %q: %s", leak, out)
		}
	}
}

func TestSMTPDriverThroughNew(t *testing.T) {
	cfg := config.NotifyConfig{Driver: "smtp", SMTP: config.SMTPConfig{
		Host: "smtp.example.com", Port: 587, From: "noreply@example.com", TLS: config.SMTPTLSStartTLS, ResetURL: testResetURL,
	}}
	n, err := New(cfg, "prod", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := n.(*SMTP); !ok {
		t.Fatalf("driver = %T, want *SMTP", n)
	}

	cfg.SMTP.TLS = config.SMTPTLSNone
	if _, err := New(cfg, "prod", nil); err == nil {
		t.Fatal("SMTP_TLS=none must be refused outside dev")
	}
	if _, err := New(cfg, "dev", nil); err != nil {
		t.Fatalf("SMTP_TLS=none in dev: %v", err)
	}
}
