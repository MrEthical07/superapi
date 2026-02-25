package tenants

import (
	"context"
	"errors"
	"testing"
	"time"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

type fakeRepo struct {
	createFn func(context.Context, CreateTenantInput) (Tenant, error)
	getFn    func(context.Context, string) (Tenant, error)
	listFn   func(context.Context, int32) ([]Tenant, error)
}

func (f *fakeRepo) Create(ctx context.Context, input CreateTenantInput) (Tenant, error) {
	return f.createFn(ctx, input)
}

func (f *fakeRepo) GetByID(ctx context.Context, id string) (Tenant, error) {
	return f.getFn(ctx, id)
}

func (f *fakeRepo) List(ctx context.Context, limit int32) ([]Tenant, error) {
	if f.listFn == nil {
		return nil, nil
	}
	return f.listFn(ctx, limit)
}

func TestServiceCreateSuccess(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepo{createFn: func(ctx context.Context, input CreateTenantInput) (Tenant, error) {
		if input.ID == "" {
			t.Fatalf("expected generated id")
		}
		return Tenant{ID: input.ID, Slug: input.Slug, Name: input.Name, Status: input.Status, CreatedAt: now, UpdatedAt: now}, nil
	}}

	s := &service{
		repo: repo,
		withTx: func(ctx context.Context, fn func(Repository) (Tenant, error)) (Tenant, error) {
			return fn(repo)
		},
	}

	got, err := s.Create(context.Background(), createTenantRequest{Slug: "Acme-Co", Name: " Acme ", Status: "ACTIVE"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.Slug != "acme-co" || got.Name != "Acme" || got.Status != "active" {
		t.Fatalf("unexpected tenant normalization: %+v", got)
	}
}

func TestServiceCreateDuplicateSlug(t *testing.T) {
	conflict := apperr.New(apperr.CodeConflict, 409, "tenant slug already exists")
	repo := &fakeRepo{createFn: func(ctx context.Context, input CreateTenantInput) (Tenant, error) {
		return Tenant{}, conflict
	}}

	s := &service{
		repo: repo,
		withTx: func(ctx context.Context, fn func(Repository) (Tenant, error)) (Tenant, error) {
			return fn(repo)
		},
	}

	_, err := s.Create(context.Background(), createTenantRequest{Slug: "acme", Name: "Acme", Status: "active"})
	if !errors.Is(err, conflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestServiceGetByIDNotFound(t *testing.T) {
	notFound := apperr.New(apperr.CodeNotFound, 404, "tenant not found")
	s := &service{repo: &fakeRepo{getFn: func(ctx context.Context, id string) (Tenant, error) {
		return Tenant{}, notFound
	}}}

	_, err := s.GetByID(context.Background(), "missing")
	if !errors.Is(err, notFound) {
		t.Fatalf("expected not_found error, got %v", err)
	}
}
