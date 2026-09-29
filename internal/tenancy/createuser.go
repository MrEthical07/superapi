package tenancy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/config"
)

// userStep is tenancy's part of cmd/createuser (app.UserStep): it validates the
// --tenant flags, makes sure the tenant exists so the new user can pass tenant
// validation at the HTTP edge, and creates the account under that tenant.
type userStep struct {
	tenantID     string
	createTenant bool
	cfg          Config
}

var _ app.UserStep = (*userStep)(nil)

// Validate implements app.UserStep.
func (s *userStep) Validate(core *config.Config) error {
	s.tenantID = strings.TrimSpace(s.tenantID)
	if s.tenantID != "" && !ValidTenantID(s.tenantID) {
		return fmt.Errorf("invalid --tenant %q", s.tenantID)
	}
	s.cfg = LoadConfig()
	if err := s.cfg.Lint(core); err != nil {
		return err
	}
	if s.cfg.Enabled && s.tenantID == "" {
		return errors.New("TENANCY_ENABLED=true requires --tenant")
	}
	if !s.cfg.Enabled && s.tenantID != "" {
		return errors.New("--tenant requires TENANCY_ENABLED=true")
	}
	return nil
}

// Prepare implements app.UserStep.
func (s *userStep) Prepare(ctx context.Context, deps *app.Dependencies) (context.Context, error) {
	if s.tenantID == "" {
		return ctx, nil
	}
	if err := s.ensureTenant(ctx, deps); err != nil {
		return ctx, err
	}
	return WithRequestTenant(ctx, s.tenantID), nil
}

// Summary implements app.UserStep.
func (s *userStep) Summary() []string {
	if s.tenantID == "" {
		return nil
	}
	return []string{fmt.Sprintf("  tenant: %s", s.tenantID)}
}

// ensureTenant makes sure the tenant exists, creating it with --create-tenant.
func (s *userStep) ensureTenant(ctx context.Context, deps *app.Dependencies) error {
	repo := NewRepository(deps.DB)
	if repo == nil {
		return errors.New("tenant repository unavailable (POSTGRES_ENABLED=false?)")
	}
	record, err := repo.Get(ctx, s.tenantID)
	switch {
	case errors.Is(err, ErrTenantNotFound):
		if !s.createTenant {
			if s.cfg.Validate {
				return fmt.Errorf("tenant %q does not exist; pass --create-tenant to create it", s.tenantID)
			}
			return nil
		}
		_, err := repo.Create(ctx, Record{ID: s.tenantID, Slug: s.tenantID, Name: s.tenantID, Status: StatusActive})
		return err
	case err != nil:
		return err
	case !record.Active():
		return fmt.Errorf("tenant %q is not active", s.tenantID)
	}
	return nil
}
