package tenants

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	coredb "github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service interface {
	Create(ctx context.Context, req createTenantRequest) (Tenant, error)
	GetByID(ctx context.Context, id string) (Tenant, error)
	List(ctx context.Context, limit int32) ([]Tenant, error)
}

type CreateTenantInput struct {
	ID     string
	Slug   string
	Name   string
	Status string
}

type txFunc func(context.Context, func(Repository) (Tenant, error)) (Tenant, error)

type service struct {
	pool   *pgxpool.Pool
	repo   Repository
	withTx txFunc
}

func NewService(pool *pgxpool.Pool, repo Repository) Service {
	if pool == nil || repo == nil {
		return &service{}
	}
	return &service{
		pool: pool,
		repo: repo,
		withTx: func(ctx context.Context, fn func(Repository) (Tenant, error)) (Tenant, error) {
			return coredb.WithTxResult(ctx, pool, func(q *sqlcgen.Queries) (Tenant, error) {
				return fn(NewRepository(q))
			})
		},
	}
}

func (s *service) Create(ctx context.Context, req createTenantRequest) (Tenant, error) {
	if s.withTx == nil {
		return Tenant{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}

	input := CreateTenantInput{
		ID:     newTenantID(),
		Slug:   strings.TrimSpace(strings.ToLower(req.Slug)),
		Name:   strings.TrimSpace(req.Name),
		Status: strings.TrimSpace(strings.ToLower(req.Status)),
	}

	return s.withTx(ctx, func(r Repository) (Tenant, error) {
		return r.Create(ctx, input)
	})
}

func (s *service) GetByID(ctx context.Context, id string) (Tenant, error) {
	if s.repo == nil {
		return Tenant{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}
	return s.repo.GetByID(ctx, id)
}

func (s *service) List(ctx context.Context, limit int32) ([]Tenant, error) {
	if s.repo == nil {
		return nil, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}
	return s.repo.List(ctx, limit)
}

func newTenantID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "tenant_00000000000000000000000000000000"
	}
	return "tenant_" + hex.EncodeToString(b[:])
}
