package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/tenant"
)

// ensureTenant makes sure the tenant exists (creating it with --create-tenant)
// so the new user can actually pass tenant validation at the HTTP edge.
func ensureTenant(ctx context.Context, deps *app.Dependencies, cfg *config.Config, opts options) error {
	repo := tenant.NewRepository(deps.DB)
	if repo == nil {
		return errors.New("tenant repository unavailable (POSTGRES_ENABLED=false?)")
	}
	record, err := repo.Get(ctx, opts.tenantID)
	switch {
	case errors.Is(err, tenant.ErrTenantNotFound):
		if !opts.createTenant {
			if cfg.Tenancy.Validate {
				return fmt.Errorf("tenant %q does not exist; pass --create-tenant to create it", opts.tenantID)
			}
			return nil
		}
		if _, err := repo.Create(ctx, tenant.Record{ID: opts.tenantID, Slug: opts.tenantID, Name: opts.tenantID, Status: tenant.StatusActive}); err != nil {
			return err
		}
		return nil
	case err != nil:
		return err
	case !record.Active():
		return fmt.Errorf("tenant %q is not active", opts.tenantID)
	}
	return nil
}
