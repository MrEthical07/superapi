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

type authTokenResponse struct {
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token"`
	AccessExpiresUTC  string `json:"access_expires_utc"`
	AccessExpiresUnix int64  `json:"access_expires_unix"`
}

// Register mounts system and auth demonstration routes.
//
// For policy behavior, see docs/policies.md.
func (m *Module) Register(r httpx.Router) error {
	r.Handle(http.MethodPost, "/system/parse-duration", httpx.Adapter(m.parseDuration))
	r.Handle(http.MethodPost, "/api/v1/system/auth/login", httpx.Adapter(m.login))
	r.Handle(http.MethodPost, "/api/v1/system/auth/refresh", httpx.Adapter(m.refresh))

	if limiter := m.runtime.Limiter(); limiter != nil {
		r.Handle(
			http.MethodGet,
			"/api/v1/system/whoami",
			httpx.Adapter(m.whoami),
			policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
			policy.RateLimitWithKeyer(limiter, "system.whoami", m.rateRule, ratelimit.KeyByUserOrTenantOrTokenHash(16)),
		)
		return nil
	}

	r.Handle(
		http.MethodGet,
		"/api/v1/system/whoami",
		httpx.Adapter(m.whoami),
		policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
	)
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
	engine, err := m.authEngine()
	if err != nil {
		return authTokenResponse{}, err
	}

	accessToken, refreshToken, err := engine.Login(ctx.Context(), strings.TrimSpace(req.Identifier), req.Password)
	if err != nil {
		return authTokenResponse{}, mapAuthEndpointError(err, "invalid credentials")
	}

	resp, err := buildAuthTokenResponse(accessToken, refreshToken)
	if err != nil {
		return authTokenResponse{}, err
	}

	return resp, nil
}

func (m *Module) refresh(ctx *httpx.Context, req authRefreshRequest) (authTokenResponse, error) {
	engine, err := m.authEngine()
	if err != nil {
		return authTokenResponse{}, err
	}

	accessToken, refreshToken, err := engine.Refresh(ctx.Context(), strings.TrimSpace(req.RefreshToken))
	if err != nil {
		return authTokenResponse{}, mapAuthEndpointError(err, "invalid refresh token")
	}

	resp, err := buildAuthTokenResponse(accessToken, refreshToken)
	if err != nil {
		return authTokenResponse{}, err
	}

	return resp, nil
}

func (m *Module) authEngine() (*goauth.Engine, error) {
	engine := m.runtime.AuthEngine()
	if engine == nil {
		return nil, apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "auth engine unavailable")
	}
	return engine, nil
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
