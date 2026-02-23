package system

import (
	"context"
	"net/http"
	"strings"
	"time"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
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
	return nil
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
