package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

// NotifyDriverSMTP is the NOTIFY_DRIVER value for the built-in SMTP notifier.
const NotifyDriverSMTP = "smtp"

// SMTP connection security modes accepted by SMTP_TLS.
const (
	// SMTPTLSStartTLS connects in the clear and upgrades with STARTTLS; the
	// upgrade is mandatory (default).
	SMTPTLSStartTLS = "starttls"
	// SMTPTLSImplicit wraps the connection in TLS from the first byte
	// (SMTPS, usually port 465).
	SMTPTLSImplicit = "implicit"
	// SMTPTLSNone sends in the clear. Only allowed with APP_ENV=dev.
	SMTPTLSNone = "none"
)

// TokenPlaceholder marks where the URL-escaped challenge goes in
// NOTIFY_RESET_URL and NOTIFY_VERIFY_URL.
const TokenPlaceholder = "{token}"

func init() { RegisterNotifyDriver(NotifyDriverSMTP) }

// SMTPConfig configures the built-in SMTP notifier (NOTIFY_DRIVER=smtp).
type SMTPConfig struct {
	// Host is the SMTP server host (SMTP_HOST).
	Host string
	// Port is the SMTP server port (SMTP_PORT); defaults by TLS mode.
	Port int
	// Username and Password enable PLAIN authentication when set
	// (SMTP_USERNAME, SMTP_PASSWORD).
	Username string
	Password string
	// From is the sender address, optionally "Name <addr>" (SMTP_FROM).
	From string
	// TLS is starttls (default), implicit, or none (SMTP_TLS).
	TLS string
	// ResetURL and VerifyURL are link templates containing {token}
	// (NOTIFY_RESET_URL, NOTIFY_VERIFY_URL).
	ResetURL  string
	VerifyURL string
}

// DefaultSMTPPort is the conventional port for a TLS mode.
func DefaultSMTPPort(mode string) int {
	switch mode {
	case SMTPTLSImplicit:
		return 465
	case SMTPTLSNone:
		return 25
	default:
		return 587
	}
}

func loadSMTP() SMTPConfig {
	mode := strings.ToLower(strings.TrimSpace(getenv("SMTP_TLS", SMTPTLSStartTLS)))
	return SMTPConfig{
		Host:      strings.TrimSpace(getenv("SMTP_HOST", "")),
		Port:      getInt("SMTP_PORT", DefaultSMTPPort(mode)),
		Username:  getenv("SMTP_USERNAME", ""),
		Password:  getenv("SMTP_PASSWORD", ""),
		From:      strings.TrimSpace(getenv("SMTP_FROM", "")),
		TLS:       mode,
		ResetURL:  strings.TrimSpace(getenv("NOTIFY_RESET_URL", "")),
		VerifyURL: strings.TrimSpace(getenv("NOTIFY_VERIFY_URL", "")),
	}
}

// lintSMTP validates every field the SMTP driver needs. It only runs when
// NOTIFY_DRIVER=smtp; the link templates are required for the features that
// use them.
func (c *Config) lintSMTP() error {
	if strings.ToLower(strings.TrimSpace(c.Notify.Driver)) != NotifyDriverSMTP {
		return nil
	}
	s := c.Notify.SMTP

	if s.Host == "" {
		return fmt.Errorf("NOTIFY_DRIVER=smtp requires SMTP_HOST")
	}
	if strings.ContainsAny(s.Host, " /\\@:?#\r\n") {
		return fmt.Errorf("SMTP_HOST must be a bare host name or IP address (no scheme, port or path): %q", s.Host)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("SMTP_PORT must be between 1 and 65535, got %d", s.Port)
	}
	switch s.TLS {
	case SMTPTLSStartTLS, SMTPTLSImplicit:
	case SMTPTLSNone:
		if !strings.EqualFold(strings.TrimSpace(c.Env), "dev") {
			return fmt.Errorf("SMTP_TLS=none sends mail and credentials in the clear and is only allowed with APP_ENV=dev")
		}
	default:
		return fmt.Errorf("invalid SMTP_TLS: %q (valid: %s, %s, %s)", s.TLS, SMTPTLSStartTLS, SMTPTLSImplicit, SMTPTLSNone)
	}
	if (s.Username == "") != (s.Password == "") {
		return fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must be set together")
	}
	if strings.ContainsAny(s.Username+s.Password, "\r\n") {
		return fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must not contain line breaks")
	}

	if s.From == "" {
		return fmt.Errorf("NOTIFY_DRIVER=smtp requires SMTP_FROM")
	}
	if strings.ContainsAny(s.From, "\r\n") {
		return fmt.Errorf("SMTP_FROM must not contain line breaks")
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		return fmt.Errorf("SMTP_FROM is not a valid address: %w", err)
	}

	if c.Auth.PasswordResetEnabled && s.ResetURL == "" {
		return fmt.Errorf("AUTH_PASSWORD_RESET_ENABLED with NOTIFY_DRIVER=smtp requires NOTIFY_RESET_URL (a link containing %s)", TokenPlaceholder)
	}
	if c.Auth.EmailVerificationEnabled && s.VerifyURL == "" {
		return fmt.Errorf("AUTH_EMAIL_VERIFICATION_ENABLED with NOTIFY_DRIVER=smtp requires NOTIFY_VERIFY_URL (a link containing %s)", TokenPlaceholder)
	}
	if s.ResetURL != "" {
		if err := ValidateLinkTemplate(s.ResetURL); err != nil {
			return fmt.Errorf("NOTIFY_RESET_URL: %w", err)
		}
	}
	if s.VerifyURL != "" {
		if err := ValidateLinkTemplate(s.VerifyURL); err != nil {
			return fmt.Errorf("NOTIFY_VERIFY_URL: %w", err)
		}
	}
	return nil
}

// ValidateLinkTemplate checks an absolute http(s) link template that contains
// {token} exactly once.
func ValidateLinkTemplate(tmpl string) error {
	if strings.ContainsAny(tmpl, " \r\n\t") {
		return fmt.Errorf("must not contain whitespace")
	}
	if n := strings.Count(tmpl, TokenPlaceholder); n != 1 {
		return fmt.Errorf("must contain %s exactly once, found %d", TokenPlaceholder, n)
	}
	// Parse with a harmless token so the braces do not trip the parser.
	u, err := url.Parse(strings.Replace(tmpl, TokenPlaceholder, "token", 1))
	if err != nil {
		return fmt.Errorf("is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be an absolute http(s) URL, got scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("must include a host")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("has an invalid port %q", port)
		}
	}
	return nil
}
