package system

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
	"github.com/golang-jwt/jwt/v5"
)

type parseDurationRequest struct {
	Duration string `json:"duration"`
}

// Validate ensures duration string is provided for parsing.
func (r parseDurationRequest) Validate() error {
	if strings.TrimSpace(r.Duration) == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "duration is required")
	}
	return nil
}

type parseDurationResponse struct {
	Duration     string `json:"duration"`
	Nanoseconds  int64  `json:"nanoseconds"`
	Milliseconds int64  `json:"milliseconds"`
}

type authLoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
	// RememberMe requests a durable session up to the configured ceiling
	// (goAuth v0.4.0 remember-me).
	RememberMe bool `json:"remember_me"`
}

// Validate ensures login credentials are present.
func (r authLoginRequest) Validate() error {
	if strings.TrimSpace(r.Identifier) == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "identifier is required")
	}
	if strings.TrimSpace(r.Password) == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "password is required")
	}
	return nil
}

type authRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Validate ensures refresh token is provided.
func (r authRefreshRequest) Validate() error {
	if strings.TrimSpace(r.RefreshToken) == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "refresh_token is required")
	}
	return nil
}

type authMFAConfirmRequest struct {
	// Challenge is the MFA session/challenge id returned by login.
	Challenge string `json:"challenge"`
	// Code is the second-factor code (for example a TOTP code).
	Code string `json:"code"`
	// Type selects the factor to complete (for example "totp" or "webauthn").
	// Optional; empty lets goAuth use the challenge's preferred factor.
	Type string `json:"type"`
}

// Validate ensures the challenge id and code are present.
func (r authMFAConfirmRequest) Validate() error {
	if strings.TrimSpace(r.Challenge) == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "challenge is required")
	}
	if strings.TrimSpace(r.Code) == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "code is required")
	}
	return nil
}

type authLogoutRequest struct {
	// AccessToken is the token whose session should be revoked. Optional in the
	// body when supplied via the Authorization header instead.
	AccessToken string `json:"access_token"`
}

type authLogoutResponse struct {
	LoggedOut bool `json:"logged_out"`
}

type authTokenResponse struct {
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token"`
	AccessExpiresUTC  string `json:"access_expires_utc"`
	AccessExpiresUnix int64  `json:"access_expires_unix"`
	// MFARequired is true when login returned an MFA challenge instead of
	// tokens. The token fields are empty in that case and the caller must
	// complete the challenge via the MFA confirm endpoint.
	MFARequired bool     `json:"mfa_required,omitempty"`
	MFAChallenge string  `json:"mfa_challenge,omitempty"`
	MFAType     string   `json:"mfa_type,omitempty"`
	MFATypes    []string `json:"mfa_types,omitempty"`
}

// Register mounts system and auth demonstration routes.
//
// For policy behavior, see docs/policies.md.
func (m *Module) Register(r httpx.Router) error {
	r.Handle(http.MethodPost, "/system/parse-duration", httpx.Adapter(m.parseDuration))
	r.Handle(http.MethodPost, "/api/v1/system/auth/login", httpx.Adapter(m.login))
	r.Handle(http.MethodPost, "/api/v1/system/auth/mfa/confirm", httpx.Adapter(m.confirmMFA))
	r.Handle(http.MethodPost, "/api/v1/system/auth/refresh", httpx.Adapter(m.refresh))
	r.Handle(http.MethodPost, "/api/v1/system/auth/logout", httpx.Adapter(m.logout))

	// whoami is registered once. Static route verification requires policies to
	// be passed directly (no variadic spread), so the two policy shapes are
	// expressed as literal r.Handle calls guarded by the limiter's presence.
	if limiter := m.runtime.Limiter(); limiter != nil {
		r.Handle(http.MethodGet, "/api/v1/system/whoami", httpx.Adapter(m.whoami),
			policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
			policy.RateLimitWithKeyer(limiter, "system.whoami", m.rateRule, ratelimit.KeyByUserOrTenantOrTokenHash(16)),
		)
	} else {
		r.Handle(http.MethodGet, "/api/v1/system/whoami", httpx.Adapter(m.whoami),
			policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
		)
	}

	m.registerWebAuthnRoutes(r)

	return nil
}

type whoamiResponse struct {
	UserID      string   `json:"user_id"`
	TenantID    string   `json:"tenant_id,omitempty"`
	Role        string   `json:"role,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

func (m *Module) whoami(ctx *httpx.Context, _ httpx.NoBody) (whoamiResponse, error) {
	principal, ok := ctx.Auth()
	if !ok {
		return whoamiResponse{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required")
	}

	return whoamiResponse{
		UserID:      principal.UserID,
		TenantID:    principal.TenantID,
		Role:        principal.Role,
		Permissions: append([]string(nil), principal.Permissions...),
	}, nil
}

func (m *Module) parseDuration(_ *httpx.Context, req parseDurationRequest) (parseDurationResponse, error) {
	d, err := time.ParseDuration(req.Duration)
	if err != nil {
		return parseDurationResponse{}, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "duration must be a valid Go duration string")
	}

	return parseDurationResponse{
		Duration:     d.String(),
		Nanoseconds:  d.Nanoseconds(),
		Milliseconds: d.Milliseconds(),
	}, nil
}

func (m *Module) login(ctx *httpx.Context, req authLoginRequest) (authTokenResponse, error) {
	outcome, err := m.auth.login(ctx.Context(), req.Identifier, req.Password, req.RememberMe)
	if err != nil {
		return authTokenResponse{}, err
	}
	return buildLoginResponse(outcome)
}

func (m *Module) confirmMFA(ctx *httpx.Context, req authMFAConfirmRequest) (authTokenResponse, error) {
	outcome, err := m.auth.confirmMFA(ctx.Context(), req.Challenge, req.Code, req.Type)
	if err != nil {
		return authTokenResponse{}, err
	}
	return buildLoginResponse(outcome)
}

func (m *Module) refresh(ctx *httpx.Context, req authRefreshRequest) (authTokenResponse, error) {
	accessToken, refreshToken, err := m.auth.refresh(ctx.Context(), req.RefreshToken)
	if err != nil {
		return authTokenResponse{}, err
	}
	return buildAuthTokenResponse(accessToken, refreshToken)
}

func (m *Module) logout(ctx *httpx.Context, req authLogoutRequest) (authLogoutResponse, error) {
	token := strings.TrimSpace(req.AccessToken)
	if token == "" {
		token = bearerToken(ctx.Header("Authorization"))
	}
	if token == "" {
		return authLogoutResponse{}, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "access token is required")
	}

	if err := m.auth.logout(ctx.Context(), token); err != nil {
		return authLogoutResponse{}, err
	}
	return authLogoutResponse{LoggedOut: true}, nil
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(header) >= len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

func mapAuthEndpointError(err error, invalidMessage string) error {
	if err == nil {
		return nil
	}

	var authErr *goauth.AuthError
	if errors.As(err, &authErr) {
		switch authErr.Category {
		case goauth.CategoryAuthAbuse:
			return apperr.WithCause(apperr.New(apperr.CodeTooManyRequests, http.StatusTooManyRequests, "authentication temporarily limited"), err)
		case goauth.CategoryAuthState:
			return apperr.WithCause(apperr.New(apperr.CodeForbidden, http.StatusForbidden, "authentication state rejected"), err)
		case goauth.CategorySystem:
			if authErr.Code == string(goauth.CodeSystemInternalError) {
				return apperr.WithCause(apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "authentication failed"), err)
			}
			return apperr.WithCause(apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "authentication unavailable"), err)
		default:
			return apperr.WithCause(apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, invalidMessage), err)
		}
	}

	if errors.Is(err, goauth.ErrLoginRateLimited) {
		return apperr.WithCause(apperr.New(apperr.CodeTooManyRequests, http.StatusTooManyRequests, "authentication temporarily limited"), err)
	}

	return apperr.WithCause(apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, invalidMessage), err)
}

// buildLoginResponse turns a login/MFA-confirm outcome into the token response.
// When the outcome is an MFA challenge it returns the challenge shape (no
// tokens); otherwise it returns the token pair with parsed expiry.
func buildLoginResponse(outcome loginOutcome) (authTokenResponse, error) {
	if outcome.MFARequired {
		return authTokenResponse{
			MFARequired:  true,
			MFAChallenge: outcome.MFASession,
			MFAType:      outcome.MFAType,
			MFATypes:     outcome.MFATypes,
		}, nil
	}
	return buildAuthTokenResponse(outcome.AccessToken, outcome.RefreshToken)
}

func buildAuthTokenResponse(accessToken, refreshToken string) (authTokenResponse, error) {
	expiresUnix, err := parseJWTExpiryUnix(accessToken)
	if err != nil {
		return authTokenResponse{}, apperr.WithCause(apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "invalid access token payload"), err)
	}

	expiresAt := time.Unix(expiresUnix, 0).UTC()
	return authTokenResponse{
		AccessToken:       accessToken,
		RefreshToken:      refreshToken,
		AccessExpiresUTC:  expiresAt.Format(time.RFC3339),
		AccessExpiresUnix: expiresUnix,
	}, nil
}

func parseJWTExpiryUnix(accessToken string) (int64, error) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(accessToken, claims); err != nil {
		return 0, err
	}

	expRaw, ok := claims["exp"]
	if !ok {
		return 0, errors.New("jwt exp claim missing")
	}

	switch exp := expRaw.(type) {
	case float64:
		return int64(exp), nil
	case int64:
		return exp, nil
	case int:
		return int64(exp), nil
	case json.Number:
		value, err := exp.Int64()
		if err != nil {
			return 0, err
		}
		return value, nil
	default:
		return 0, errors.New("jwt exp claim has invalid type")
	}
}
