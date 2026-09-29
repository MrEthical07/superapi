package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	coreauth "github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// Input limits. Passwords are hashed with Argon2, so an unbounded password is
// a cheap CPU amplification vector; identifiers end up in keys and logs.
const (
	maxIdentifierLength = 320
	maxPasswordLength   = 1024
	maxChallengeLength  = 1024
	maxCodeLength       = 64
)

func badRequest(msg string) error {
	return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, msg)
}

func requireField(value, name string, maxLen int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return badRequest(name + " is required")
	}
	if len(value) > maxLen {
		return badRequest(name + " is too long")
	}
	return nil
}

// --- login / MFA / refresh / logout ---

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
	// RememberMe requests a durable session up to the configured ceiling.
	RememberMe bool `json:"remember_me"`
}

// Validate ensures login credentials are present.
func (r loginRequest) Validate() error {
	if err := requireField(r.Identifier, "identifier", maxIdentifierLength); err != nil {
		return err
	}
	return requireField(r.Password, "password", maxPasswordLength)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Validate ensures refresh token is provided.
func (r refreshRequest) Validate() error {
	return requireField(r.RefreshToken, "refresh_token", 4096)
}

type mfaConfirmRequest struct {
	// Challenge is the MFA challenge id returned by login.
	Challenge string `json:"challenge"`
	// Code is the second-factor code (TOTP or backup code).
	Code string `json:"code"`
	// Type selects the factor ("totp", "backup", "webauthn"). Optional.
	Type string `json:"type"`
}

// Validate ensures the challenge id and code are present.
func (r mfaConfirmRequest) Validate() error {
	if err := requireField(r.Challenge, "challenge", maxChallengeLength); err != nil {
		return err
	}
	return requireField(r.Code, "code", 8192)
}

type logoutRequest struct {
	// AccessToken is the token whose session should be revoked. Optional in
	// the body when supplied via the Authorization header instead.
	AccessToken string `json:"access_token"`
}

type logoutResponse struct {
	LoggedOut bool `json:"logged_out"`
}

type tokenResponse struct {
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token"`
	AccessExpiresUTC  string `json:"access_expires_utc"`
	AccessExpiresUnix int64  `json:"access_expires_unix"`
	// MFARequired is true when login returned an MFA challenge instead of
	// tokens; complete it via POST /api/v1/auth/mfa/confirm.
	MFARequired  bool     `json:"mfa_required,omitempty"`
	MFAChallenge string   `json:"mfa_challenge,omitempty"`
	MFAType      string   `json:"mfa_type,omitempty"`
	MFATypes     []string `json:"mfa_types,omitempty"`
}

// whoamiResponse describes the authenticated principal. Attributes are the
// values optional features attached to it (see policy.AuthExtension); each is
// reported as an extra top-level field under its own key, after user_id, so a
// feature that is not registered adds nothing.
type whoamiResponse struct {
	UserID      string
	Role        string
	Permissions []string
	Attributes  []coreauth.Attribute
}

// MarshalJSON emits user_id, the feature attributes, then role and
// permissions. An attribute with an empty value, or whose key collides with a
// core field, is skipped.
func (w whoamiResponse) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	field := func(key string, value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(key)
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(encoded)
		return nil
	}
	if err := field("user_id", w.UserID); err != nil {
		return nil, err
	}
	reserved := map[string]bool{"user_id": true, "role": true, "permissions": true}
	for _, attr := range w.Attributes {
		if attr.Value == "" || reserved[attr.Key] {
			continue
		}
		reserved[attr.Key] = true
		if err := field(attr.Key, attr.Value); err != nil {
			return nil, err
		}
	}
	if w.Role != "" {
		if err := field("role", w.Role); err != nil {
			return nil, err
		}
	}
	if len(w.Permissions) > 0 {
		if err := field("permissions", w.Permissions); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// --- registration ---

type registerRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
	// RememberMe applies only when AUTH_REGISTRATION_AUTO_LOGIN is on.
	RememberMe bool `json:"remember_me"`
}

// Validate ensures identifier and password are present and bounded.
func (r registerRequest) Validate() error {
	if err := requireField(r.Identifier, "identifier", maxIdentifierLength); err != nil {
		return err
	}
	return requireField(r.Password, "password", maxPasswordLength)
}

// acceptedResponse is the single, enumeration-safe body returned by
// registration and the reset/verification request endpoints.
type acceptedResponse struct {
	Accepted bool `json:"accepted"`
}

// --- password reset ---

type identifierRequest struct {
	Identifier string `json:"identifier"`
}

// Validate ensures the identifier is present and bounded.
func (r identifierRequest) Validate() error {
	return requireField(r.Identifier, "identifier", maxIdentifierLength)
}

type passwordResetConfirmRequest struct {
	Challenge   string `json:"challenge"`
	NewPassword string `json:"new_password"`
	// MFAType/MFACode are required when the account has TOTP enabled and
	// goAuth is configured to require it for resets ("totp" or "backup").
	MFAType string `json:"mfa_type"`
	MFACode string `json:"mfa_code"`
}

// Validate ensures the challenge and new password are present.
func (r passwordResetConfirmRequest) Validate() error {
	if err := requireField(r.Challenge, "challenge", maxChallengeLength); err != nil {
		return err
	}
	if err := requireField(r.NewPassword, "new_password", maxPasswordLength); err != nil {
		return err
	}
	if len(r.MFACode) > maxCodeLength {
		return badRequest("mfa_code is too long")
	}
	return nil
}

// --- email verification ---

type emailVerifyConfirmRequest struct {
	// Challenge is the full challenge from the verification message. Use it
	// alone, or send VerificationID + Code instead.
	Challenge      string `json:"challenge"`
	VerificationID string `json:"verification_id"`
	Code           string `json:"code"`
}

// Validate requires either a challenge or a verification id + code pair.
func (r emailVerifyConfirmRequest) Validate() error {
	hasChallenge := strings.TrimSpace(r.Challenge) != ""
	hasPair := strings.TrimSpace(r.VerificationID) != "" || strings.TrimSpace(r.Code) != ""
	switch {
	case hasChallenge && hasPair:
		return badRequest("send either challenge or verification_id and code, not both")
	case hasChallenge:
		return requireField(r.Challenge, "challenge", maxChallengeLength)
	default:
		if err := requireField(r.VerificationID, "verification_id", maxChallengeLength); err != nil {
			return err
		}
		return requireField(r.Code, "code", maxChallengeLength)
	}
}

type confirmedResponse struct {
	Confirmed bool `json:"confirmed"`
}

// --- password change / sessions ---

type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// Validate ensures both passwords are present.
func (r passwordChangeRequest) Validate() error {
	if err := requireField(r.CurrentPassword, "current_password", maxPasswordLength); err != nil {
		return err
	}
	return requireField(r.NewPassword, "new_password", maxPasswordLength)
}

type changedResponse struct {
	Changed bool `json:"changed"`
}

type sessionView struct {
	SessionID  string `json:"session_id"`
	CreatedUTC string `json:"created_utc,omitempty"`
	ExpiresUTC string `json:"expires_utc,omitempty"`
}

type sessionsResponse struct {
	Sessions []sessionView `json:"sessions"`
}

// --- TOTP / backup codes ---

type totpSetupResponse struct {
	// SecretBase32 is shown once so the user can type it into an
	// authenticator app; OTPAuthURI can be rendered as a QR code.
	SecretBase32 string `json:"secret_base32"`
	OTPAuthURI   string `json:"otpauth_uri"`
}

// stepUpRequest carries the current password for sensitive actions. A stolen
// access token alone must not be enough to add or remove a second factor.
type stepUpRequest struct {
	Password string `json:"password"`
}

// Validate ensures the password is present and bounded.
func (r stepUpRequest) Validate() error {
	return requirePassword(r.Password)
}

// requirePassword checks the step-up password field. The value is never
// trimmed: whitespace is a legal part of a password.
func requirePassword(password string) error {
	if password == "" {
		return badRequest("password is required")
	}
	if len(password) > maxPasswordLength {
		return badRequest("password is too long")
	}
	return nil
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

// Validate ensures the TOTP code is present and bounded.
func (r totpCodeRequest) Validate() error {
	return requireField(r.Code, "code", maxCodeLength)
}

type totpConfirmResponse struct {
	Enabled bool `json:"enabled"`
	// BackupCodes are shown exactly once; only their hashes are stored.
	BackupCodes []string `json:"backup_codes"`
}

type totpDisableResponse struct {
	Disabled bool `json:"disabled"`
}

type backupCodesResponse struct {
	BackupCodes []string `json:"backup_codes"`
}
