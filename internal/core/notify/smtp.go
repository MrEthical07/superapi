package notify

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

func init() {
	RegisterDriver(config.NotifyDriverSMTP, func(cfg config.NotifyConfig, env string, _ *logx.Logger) (Notifier, error) {
		if cfg.SMTP.TLS == config.SMTPTLSNone && !strings.EqualFold(strings.TrimSpace(env), "dev") {
			return nil, errors.New("smtp: SMTP_TLS=none is only allowed with APP_ENV=dev")
		}
		return NewSMTP(cfg.SMTP)
	})
}

// Fixed message text. Messages are short, plain and neutral; the only variable
// part is the link.
const (
	subjectPasswordReset     = "Reset your password"
	subjectEmailVerification = "Verify your email address"

	bodyPasswordReset = "We received a request to reset the password for your account.\n" +
		"\n" +
		"To choose a new password, open this link:\n" +
		"\n" +
		"%s\n" +
		"\n" +
		"If you did not ask for this, ignore this message. Your password will not change.\n"

	bodyEmailVerification = "Please confirm this email address for your account.\n" +
		"\n" +
		"To confirm it, open this link:\n" +
		"\n" +
		"%s\n" +
		"\n" +
		"If you did not create an account, ignore this message.\n"
)

// SMTP delivers reset and verification messages over SMTP using only the
// standard library (net/smtp, crypto/tls).
//
// Connections use STARTTLS (mandatory: the message is never sent in the clear
// if the server does not offer it), implicit TLS, or, in development only, no
// encryption. The challenge and the full link are never logged or placed in an
// error; addresses in errors are redacted with RedactAddress.
type SMTP struct {
	cfg  config.SMTPConfig
	from *mail.Address

	// tlsConfig, dial, now and randRead are seams for tests.
	tlsConfig *tls.Config
	dial      func(ctx context.Context, network, address string) (net.Conn, error)
	now       func() time.Time
	randRead  func([]byte) (int, error)
}

// NewSMTP validates cfg and returns an SMTP notifier.
func NewSMTP(cfg config.SMTPConfig) (*SMTP, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, errors.New("smtp: SMTP_HOST is required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("smtp: SMTP_PORT must be between 1 and 65535, got %d", cfg.Port)
	}
	switch cfg.TLS {
	case config.SMTPTLSStartTLS, config.SMTPTLSImplicit, config.SMTPTLSNone:
	default:
		return nil, fmt.Errorf("smtp: invalid SMTP_TLS %q", cfg.TLS)
	}
	if strings.ContainsAny(cfg.From, "\r\n") {
		return nil, errors.New("smtp: SMTP_FROM must not contain line breaks")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp: SMTP_FROM is not a valid address: %w", err)
	}
	for name, tmpl := range map[string]string{"NOTIFY_RESET_URL": cfg.ResetURL, "NOTIFY_VERIFY_URL": cfg.VerifyURL} {
		if tmpl == "" {
			continue
		}
		if err := config.ValidateLinkTemplate(tmpl); err != nil {
			return nil, fmt.Errorf("smtp: %s: %w", name, err)
		}
	}
	dialer := &net.Dialer{}
	return &SMTP{
		cfg:      cfg,
		from:     from,
		dial:     dialer.DialContext,
		now:      time.Now,
		randRead: rand.Read,
	}, nil
}

// SendPasswordReset delivers a password-reset link to the address.
func (s *SMTP) SendPasswordReset(ctx context.Context, to, challenge string) error {
	link, err := buildLink(s.cfg.ResetURL, challenge)
	if err != nil {
		return fmt.Errorf("smtp: password reset link: %w (set NOTIFY_RESET_URL)", err)
	}
	return s.send(ctx, to, subjectPasswordReset, fmt.Sprintf(bodyPasswordReset, link))
}

// SendEmailVerification delivers an email-verification link to the address.
func (s *SMTP) SendEmailVerification(ctx context.Context, to, challenge string) error {
	link, err := buildLink(s.cfg.VerifyURL, challenge)
	if err != nil {
		return fmt.Errorf("smtp: email verification link: %w (set NOTIFY_VERIFY_URL)", err)
	}
	return s.send(ctx, to, subjectEmailVerification, fmt.Sprintf(bodyEmailVerification, link))
}

// buildLink substitutes the URL-escaped challenge for {token}. The escaping is
// safe in a path segment, a query value and a fragment: every reserved
// character is percent-encoded (a space becomes %20, not '+').
func buildLink(template, challenge string) (string, error) {
	if template == "" {
		return "", errors.New("link template is empty")
	}
	if challenge == "" {
		return "", errors.New("challenge is empty")
	}
	escaped := strings.ReplaceAll(url.QueryEscape(challenge), "+", "%20")
	return strings.Replace(template, config.TokenPlaceholder, escaped, 1), nil
}

func (s *SMTP) send(ctx context.Context, to, subject, body string) error {
	recipient, err := parseRecipient(to)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	msg, err := s.buildMessage(recipient, subject, body)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if err := s.deliver(ctx, recipient, msg); err != nil {
		return scrub(err, to, recipient)
	}
	return nil
}

// parseRecipient accepts a single bare address and rejects anything that could
// smuggle extra headers or recipients.
func parseRecipient(to string) (string, error) {
	if strings.ContainsAny(to, "\r\n\x00") {
		return "", errors.New("recipient contains a line break")
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(to))
	if err != nil {
		return "", errors.New("recipient is not a valid address")
	}
	if strings.ContainsAny(addr.Address, " \t<>,;") {
		return "", errors.New("recipient is not a valid address")
	}
	return addr.Address, nil
}

// buildMessage renders the RFC 5322 message. Header values are checked for
// line breaks so no caller-supplied text can start a new header.
func (s *SMTP) buildMessage(to, subject, body string) ([]byte, error) {
	for name, v := range map[string]string{"recipient": to, "subject": subject} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("%s contains a line break", name)
		}
	}
	for i := 0; i < len(body); i++ {
		if body[i] > 0x7e || (body[i] < 0x20 && body[i] != '\n' && body[i] != '\t') {
			return nil, errors.New("message body must be plain ASCII text")
		}
	}

	var id [16]byte
	if _, err := s.randRead(id[:]); err != nil {
		return nil, fmt.Errorf("message id: %w", err)
	}
	domain := "localhost"
	if _, d, ok := strings.Cut(s.from.Address, "@"); ok && d != "" {
		domain = d
	}

	headers := []string{
		"From: " + s.from.String(),
		"To: " + to,
		"Subject: " + subject,
		"Date: " + s.now().UTC().Format(time.RFC1123Z),
		"Message-ID: <" + hex.EncodeToString(id[:]) + "@" + domain + ">",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 7bit",
		"Auto-Submitted: auto-generated",
	}
	text := strings.ReplaceAll(body, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + text), nil
}

func (s *SMTP) tlsClientConfig() *tls.Config {
	if s.tlsConfig != nil {
		cfg := s.tlsConfig.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName = s.cfg.Host
		}
		return cfg
	}
	return &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
}

func (s *SMTP) deliver(ctx context.Context, to string, msg []byte) error {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	conn, err := s.dial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	// Bound every read and write by the delivery deadline, and unblock a
	// stuck call if the context is cancelled.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if s.cfg.TLS == config.SMTPTLSImplicit {
		tlsConn := tls.Client(conn, s.tlsClientConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("tls handshake: %w", err)
		}
		conn = tlsConn
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	if s.cfg.TLS == config.SMTPTLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("server does not offer STARTTLS; refusing to send without encryption")
		}
		if err := c.StartTLS(s.tlsClientConfig()); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("server does not offer AUTH but SMTP_USERNAME is set")
		}
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish message: %w", err)
	}
	return c.Quit()
}

// scrubbedError carries an error whose text has had recipient addresses
// redacted, while keeping the original for errors.Is/As.
type scrubbedError struct {
	msg   string
	cause error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.cause }

// scrub redacts every form of the recipient address in err's text: SMTP
// servers commonly echo the address in their rejection replies.
func scrub(err error, addresses ...string) error {
	msg := "smtp: " + err.Error()
	for _, a := range addresses {
		if a = strings.TrimSpace(a); a != "" {
			msg = strings.ReplaceAll(msg, a, RedactAddress(a))
		}
	}
	return &scrubbedError{msg: msg, cause: err}
}
