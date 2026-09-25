package system

import (
	"net/http"
	"strings"
	"time"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
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

// Register mounts system utility routes. Auth routes live in
// internal/modules/auth.
func (m *Module) Register(r httpx.Router) error {
	r.Handle(http.MethodPost, "/system/parse-duration", httpx.Adapter(m.parseDuration))
	return nil
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
