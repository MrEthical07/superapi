package tenants

import (
	"context"
	"errors"
	"time"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Tenant struct {
	ID        string
	Slug      string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repository interface {
	Create(ctx context.Context, input CreateTenantInput) (Tenant, error)
	GetByID(ctx context.Context, id string) (Tenant, error)
	List(ctx context.Context, limit int32) ([]Tenant, error)
}

type repository struct {
	q *sqlcgen.Queries
}

func NewRepository(q *sqlcgen.Queries) Repository {
	return &repository{q: q}
}

func (r *repository) Create(ctx context.Context, input CreateTenantInput) (Tenant, error) {
	row, err := r.q.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID:     input.ID,
		Slug:   input.Slug,
		Name:   input.Name,
		Status: input.Status,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Tenant{}, apperr.New(apperr.CodeConflict, 409, "tenant slug already exists")
		}
		return Tenant{}, err
	}
	return fromRow(row), nil
}

func (r *repository) GetByID(ctx context.Context, id string) (Tenant, error) {
	row, err := r.q.GetTenantByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, apperr.New(apperr.CodeNotFound, 404, "tenant not found")
		}
		return Tenant{}, err
	}
	return fromRow(row), nil
}

func (r *repository) List(ctx context.Context, limit int32) ([]Tenant, error) {
	rows, err := r.q.ListTenants(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]Tenant, 0, len(rows))
	for _, row := range rows {
		items = append(items, fromRow(row))
	}
	return items, nil
}

func fromRow(row sqlcgen.Tenant) Tenant {
	return Tenant{
		ID:        row.ID,
		Slug:      row.Slug,
		Name:      row.Name,
		Status:    row.Status,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}
