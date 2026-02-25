package tenants

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

const (
	tenantStatusActive   = "active"
	tenantStatusInactive = "inactive"

	defaultListLimit = 50
	maxListLimit     = 100
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type createTenantRequest struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (r createTenantRequest) Validate() error {
	slug := strings.TrimSpace(strings.ToLower(r.Slug))
	name := strings.TrimSpace(r.Name)
	status := strings.TrimSpace(strings.ToLower(r.Status))

	if slug == "" {
		return apperr.New(apperr.CodeBadRequest, 400, "slug is required")
	}
	if len(slug) > 63 || !slugPattern.MatchString(slug) {
		return apperr.New(apperr.CodeBadRequest, 400, "slug must match ^[a-z0-9]+(?:-[a-z0-9]+)*$ and be <= 63 chars")
	}
	if name == "" {
		return apperr.New(apperr.CodeBadRequest, 400, "name is required")
	}
	if len(name) > 120 {
		return apperr.New(apperr.CodeBadRequest, 400, "name must be <= 120 chars")
	}
	if status != tenantStatusActive && status != tenantStatusInactive {
		return apperr.New(apperr.CodeBadRequest, 400, "status must be one of: active, inactive")
	}
	return nil
}

type tenantResponse struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type listTenantsResponse struct {
	Items []tenantResponse `json:"items"`
	Count int              `json:"count"`
	Limit int              `json:"limit"`
}

func parseListLimit(raw string) (int32, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultListLimit, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, apperr.New(apperr.CodeBadRequest, 400, "limit must be a valid integer")
	}
	if limit <= 0 || limit > maxListLimit {
		return 0, apperr.New(apperr.CodeBadRequest, 400, "limit must be between 1 and 100")
	}
	return int32(limit), nil
}
