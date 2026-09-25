package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	goauth "github.com/MrEthical07/goAuth"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// messenger schedules out-of-band delivery. *notify.Dispatcher implements it
// asynchronously so response timing never depends on delivery.
type messenger interface {
	PasswordReset(ctx context.Context, to, challenge string)
	EmailVerification(ctx context.Context, to, challenge string)
}

// features is the subset of auth config the service needs.
type features struct {
	Registration      bool
	AutoLogin         bool
	PasswordReset     bool
	EmailVerification bool
	TOTP              bool
}

// service owns every auth workflow. It is the only code in the module that
// calls the goAuth engine; handlers stay transport-only.
type service struct {
	engine    *goauth.Engine
	accounts  accountRepository
	messenger messenger
	features  features
}

func newService(engine *goauth.Engine, accounts accountRepository, m messenger, f features) *service {
	return &service{engine: engine, accounts: accounts, messenger: m, features: f}
}

func (s *service) requireEngine() (*goauth.Engine, error) {
	if s == nil || s.engine == nil {
		return nil, apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "auth engine unavailable")
	}
	return s.engine, nil
}

// loginOutcome carries the result of a login attempt: either tokens, or an MFA
// challenge the caller must complete.
type loginOutcome struct {
	AccessToken  string
	RefreshToken string
	MFARequired  bool
	MFAType      string
	MFASession   string
	MFATypes     []string
}

func outcomeFrom(result *goauth.LoginResult) loginOutcome {
	if result == nil {
		return loginOutcome{}
	}
	return loginOutcome{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		MFARequired:  result.MFARequired,
		MFAType:      result.MFAType,
		MFASession:   result.MFASession,
		MFATypes:     append([]string(nil), result.MFATypes...),
	}
}

// --- login / MFA / refresh / logout ---

func (s *service) login(ctx context.Context, identifier, password string, rememberMe bool) (loginOutcome, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return loginOutcome{}, err
	}
	result, err := engine.LoginWithOptions(ctx, strings.TrimSpace(identifier), password, goauth.LoginOptions{RememberMe: rememberMe})
	if err != nil {
		return loginOutcome{}, mapAuthEndpointError(err, "invalid credentials")
	}
	return outcomeFrom(result), nil
}

func (s *service) confirmMFA(ctx context.Context, challengeID, code, mfaType string) (loginOutcome, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return loginOutcome{}, err
	}
	result, err := engine.ConfirmLoginMFAWithType(ctx, strings.TrimSpace(challengeID), strings.TrimSpace(code), strings.TrimSpace(mfaType))
	if err != nil {
		return loginOutcome{}, mapAuthEndpointError(err, "invalid mfa challenge")
	}
	return outcomeFrom(result), nil
}

func (s *service) refresh(ctx context.Context, refreshToken string) (string, string, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return "", "", err
	}
	access, next, err := engine.Refresh(ctx, strings.TrimSpace(refreshToken))
	if err != nil {
		return "", "", mapAuthEndpointError(err, "invalid refresh token")
	}
	return access, next, nil
}

// logout revokes the session behind an access token. goAuth accepts an
// expired-but-authentic token; only a structurally invalid one is rejected.
func (s *service) logout(ctx context.Context, accessToken string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if err := engine.LogoutByAccessToken(ctx, strings.TrimSpace(accessToken)); err != nil {
		return mapAuthEndpointError(err, "invalid access token")
	}
	return nil
}

func (s *service) logoutAll(ctx context.Context, userID string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if err := engine.LogoutAll(ctx, userID); err != nil {
		return mapFlowError(err)
	}
	return nil
}

func (s *service) listSessions(ctx context.Context, userID string) ([]goauth.SessionInfo, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	sessions, err := engine.ListActiveSessions(ctx, userID)
	if err != nil {
		return nil, mapFlowError(err)
	}
	return sessions, nil
}

func (s *service) changePassword(ctx context.Context, userID, current, next string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if err := engine.ChangePassword(ctx, userID, current, next); err != nil {
		return mapFlowError(err)
	}
	return nil
}

// --- registration ---

// registerOutcome is empty (enumeration-safe accept) unless auto-login issued
// tokens for a newly created account.
type registerOutcome struct {
	AccessToken  string
	RefreshToken string
}

// register creates an account with the default role. It is enumeration-safe:
// an identifier that already exists yields the same empty outcome as a new
// one (goAuth hashes the password before the duplicate is detected, so timing
// is comparable). With email verification on, a verification message is
// requested for both cases and delivered only to a pending account.
//
// With auto-login on, a new account returns tokens while an existing one does
// not; that mode deliberately trades enumeration resistance for convenience.
func (s *service) register(ctx context.Context, identifier, password string, rememberMe bool) (registerOutcome, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return registerOutcome{}, err
	}
	identifier = strings.TrimSpace(identifier)

	result, err := engine.CreateAccount(ctx, goauth.CreateAccountRequest{
		Identifier: identifier,
		Password:   password,
		RememberMe: rememberMe,
	})
	created := err == nil
	if err != nil && !errors.Is(err, goauth.ErrAccountExists) {
		return registerOutcome{}, mapFlowError(err)
	}

	if s.features.EmailVerification {
		if err := s.sendEmailVerification(ctx, identifier); err != nil {
			return registerOutcome{}, err
		}
	}

	if !created || result == nil || !s.features.AutoLogin {
		return registerOutcome{}, nil
	}
	return registerOutcome{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken}, nil
}

// --- password reset ---

// requestPasswordReset asks goAuth for a reset challenge and delivers it only
// when the account exists. goAuth returns a synthetic challenge for unknown
// identifiers (with an equalizing delay), so the caller always sees success;
// only rate limiting and backend failures surface as errors.
func (s *service) requestPasswordReset(ctx context.Context, identifier string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	identifier = strings.TrimSpace(identifier)
	challenge, err := engine.RequestPasswordReset(ctx, identifier)
	if err != nil {
		return mapFlowError(err)
	}
	return s.deliver(ctx, identifier, func(r recipient) bool { return true }, func(to string) {
		s.messenger.PasswordReset(ctx, to, challenge)
	})
}

func (s *service) confirmPasswordReset(ctx context.Context, challenge, newPassword, mfaType, mfaCode string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	challenge = strings.TrimSpace(challenge)
	if code := strings.TrimSpace(mfaCode); code != "" {
		mfaType = strings.TrimSpace(mfaType)
		if mfaType == "" {
			mfaType = "totp"
		}
		err = engine.ConfirmPasswordResetWithMFA(ctx, challenge, newPassword, mfaType, code)
	} else {
		err = engine.ConfirmPasswordReset(ctx, challenge, newPassword)
	}
	if err != nil {
		return mapFlowError(err)
	}
	return nil
}

// --- email verification ---

func (s *service) requestEmailVerification(ctx context.Context, identifier string) error {
	if _, err := s.requireEngine(); err != nil {
		return err
	}
	return s.sendEmailVerification(ctx, strings.TrimSpace(identifier))
}

// sendEmailVerification requests a verification challenge and delivers it only
// to an account that is pending verification — the only case in which goAuth
// stores a real record. Every other case gets a synthetic challenge.
func (s *service) sendEmailVerification(ctx context.Context, identifier string) error {
	challenge, err := s.engine.RequestEmailVerification(ctx, identifier)
	if err != nil {
		return mapFlowError(err)
	}
	return s.deliver(ctx, identifier, func(r recipient) bool { return r.PendingVerification }, func(to string) {
		s.messenger.EmailVerification(ctx, to, challenge)
	})
}

func (s *service) confirmEmailVerification(ctx context.Context, challenge, verificationID, code string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if challenge = strings.TrimSpace(challenge); challenge != "" {
		err = engine.ConfirmEmailVerification(ctx, challenge)
	} else {
		err = engine.ConfirmEmailVerificationCode(ctx, strings.TrimSpace(verificationID), strings.TrimSpace(code))
	}
	if err != nil {
		return mapFlowError(err)
	}
	return nil
}

// deliver looks up the recipient and, when eligible, schedules delivery.
// Delivery is asynchronous, so whether a message was sent does not change the
// response time.
func (s *service) deliver(ctx context.Context, identifier string, eligible func(recipient) bool, send func(to string)) error {
	if s.accounts == nil || s.messenger == nil {
		return nil
	}
	r, found, err := s.accounts.FindRecipient(ctx, identifier)
	if err != nil {
		return apperr.WithCause(apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "authentication unavailable"), err)
	}
	if found && eligible(r) && r.Address != "" {
		send(r.Address)
	}
	return nil
}

// --- TOTP / backup codes ---

type totpSetup struct {
	SecretBase32 string
	OTPAuthURI   string
}

// setupTOTP starts TOTP enrollment. It refuses while TOTP is already enabled:
// goAuth would otherwise replace the active secret before the new one is
// confirmed, silently breaking the user's existing authenticator.
func (s *service) setupTOTP(ctx context.Context, userID string) (totpSetup, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return totpSetup{}, err
	}
	if s.accounts == nil {
		return totpSetup{}, apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "authentication unavailable")
	}
	alreadyEnabled, err := s.accounts.TOTPEnabled(ctx, userID)
	if err != nil {
		return totpSetup{}, mapFlowError(goauth.ErrUserNotFound)
	}
	if alreadyEnabled {
		return totpSetup{}, apperr.New(apperr.CodeConflict, http.StatusConflict, "totp is already enabled; disable it before enrolling again")
	}
	setup, err := engine.GenerateTOTPSetup(ctx, userID)
	if err != nil {
		return totpSetup{}, mapFlowError(err)
	}
	return totpSetup{SecretBase32: setup.SecretBase32, OTPAuthURI: setup.QRCodeURL}, nil
}

// confirmTOTP verifies the first code, enables TOTP (goAuth then revokes the
// user's sessions), and issues the initial backup codes.
func (s *service) confirmTOTP(ctx context.Context, userID, code string) ([]string, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	if err := engine.ConfirmTOTPSetup(ctx, userID, strings.TrimSpace(code)); err != nil {
		return nil, mapFlowError(err)
	}
	codes, err := engine.GenerateBackupCodes(ctx, userID)
	if err != nil {
		return nil, mapFlowError(err)
	}
	return codes, nil
}

// disableTOTP requires a current TOTP code before turning TOTP off.
func (s *service) disableTOTP(ctx context.Context, userID, code string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if err := engine.VerifyTOTP(ctx, userID, strings.TrimSpace(code)); err != nil {
		return mapFlowError(err)
	}
	if err := engine.DisableTOTP(ctx, userID); err != nil {
		return mapFlowError(err)
	}
	return nil
}

func (s *service) regenerateBackupCodes(ctx context.Context, userID, code string) ([]string, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	codes, err := engine.RegenerateBackupCodes(ctx, userID, strings.TrimSpace(code))
	if err != nil {
		return nil, mapFlowError(err)
	}
	return codes, nil
}

// --- WebAuthn ceremonies (return an error while WEBAUTHN_ENABLED=false) ---

func (s *service) beginWebAuthnRegistration(ctx context.Context, userID string) (*goauth.WebAuthnRegistrationChallenge, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	challenge, err := engine.BeginWebAuthnRegistration(ctx, userID)
	if err != nil {
		return nil, mapAuthEndpointError(err, "webauthn registration unavailable")
	}
	return challenge, nil
}

func (s *service) finishWebAuthnRegistration(ctx context.Context, userID, ceremonyID string, responseJSON []byte) (*goauth.WebAuthnCredential, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	cred, err := engine.FinishWebAuthnRegistration(ctx, userID, ceremonyID, responseJSON)
	if err != nil {
		return nil, mapAuthEndpointError(err, "webauthn registration failed")
	}
	return cred, nil
}

func (s *service) listWebAuthnCredentials(ctx context.Context, userID string) ([]goauth.WebAuthnCredential, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	creds, err := engine.ListWebAuthnCredentials(ctx, userID)
	if err != nil {
		return nil, mapAuthEndpointError(err, "webauthn unavailable")
	}
	return creds, nil
}

func (s *service) removeWebAuthnCredential(ctx context.Context, userID string, credentialID []byte) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if err := engine.RemoveWebAuthnCredential(ctx, userID, credentialID); err != nil {
		return mapAuthEndpointError(err, "webauthn credential removal failed")
	}
	return nil
}
