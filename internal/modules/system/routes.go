package system

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
	"github.com/MrEthical07/superapi/internal/core/response"
)

type parseDurationRequest struct {
	Duration string `json:"duration"`
}

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

func (m *Module) Register(r httpx.Router) error {
	r.Handle(http.MethodPost, "/system/parse-duration", httpx.JSON(m.parseDuration))
	r.Handle(
		http.MethodGet,
		"/api/v1/system/whoami",
		http.HandlerFunc(m.whoami),
		policy.AuthRequired(m.authProvider, m.authMode),
		policy.RateLimitWithKeyer(m.limiter, "system.whoami", m.rateRule, ratelimit.KeyByUserOrTenantOrTokenHash(16)),
	)
	return nil
}

type whoamiResponse struct {
	UserID      string   `json:"user_id"`
	TenantID    string   `json:"tenant_id,omitempty"`
	Role        string   `json:"role,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

func (m *Module) whoami(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	response.OK(w, whoamiResponse{
		UserID:      principal.UserID,
		TenantID:    principal.TenantID,
		Role:        principal.Role,
		Permissions: append([]string(nil), principal.Permissions...),
	}, httpx.RequestIDFromContext(r.Context()))
}

func (m *Module) parseDuration(ctx context.Context, req parseDurationRequest) (parseDurationResponse, error) {
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
