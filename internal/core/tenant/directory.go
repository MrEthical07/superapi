package tenant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

// ErrTenantNotFound is returned when a tenant id does not exist.
var ErrTenantNotFound = errors.New("tenant not found")

// Tenant status values stored in tenants.status.
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// Record is the domain projection of a tenants row.
type Record struct {
	ID     string
	Slug   string
	Name   string
	Status string
}

// Active reports whether the tenant may serve requests.
func (r Record) Active() bool { return r.Status == StatusActive }

// Directory looks up tenants. The tenant middleware uses it to validate the
// resolved request tenant (TENANCY_VALIDATE).
type Directory interface {
	// Get returns the tenant or ErrTenantNotFound.
	Get(ctx context.Context, tenantID string) (Record, error)
}

// Repository persists tenants over the relational boundary. It satisfies
// Directory and is also used by tooling that seeds tenants.
type Repository interface {
	Directory
	Create(ctx context.Context, record Record) (Record, error)
}

type sqlcRepository struct {
	pg *storage.Postgres
}

// NewRepository creates a tenant repository backed by sqlc queries.
func NewRepository(pg *storage.Postgres) Repository {
	if pg == nil {
		return nil
	}
	return &sqlcRepository{pg: pg}
}

func (r *sqlcRepository) Get(ctx context.Context, tenantID string) (Record, error) {
	row, err := r.pg.Queries(ctx).GetTenantByID(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, ErrTenantNotFound
		}
		return Record{}, fmt.Errorf("get tenant: %w", err)
	}
	return Record{ID: row.ID, Slug: row.Slug, Name: row.Name, Status: row.Status}, nil
}

func (r *sqlcRepository) Create(ctx context.Context, record Record) (Record, error) {
	status := strings.TrimSpace(record.Status)
	if status == "" {
		status = StatusActive
	}
	row, err := r.pg.Queries(ctx).CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID:     strings.TrimSpace(record.ID),
		Slug:   strings.TrimSpace(record.Slug),
		Name:   strings.TrimSpace(record.Name),
		Status: status,
	})
	if err != nil {
		return Record{}, fmt.Errorf("create tenant: %w", err)
	}
	return Record{ID: row.ID, Slug: row.Slug, Name: row.Name, Status: row.Status}, nil
}
