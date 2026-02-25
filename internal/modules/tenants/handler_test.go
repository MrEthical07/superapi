package tenants

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/httpx"
)

type fakeService struct {
	createFn func(context.Context, createTenantRequest) (Tenant, error)
	getFn    func(context.Context, string) (Tenant, error)
	listFn   func(context.Context, int32) ([]Tenant, error)
}

func (f *fakeService) Create(ctx context.Context, req createTenantRequest) (Tenant, error) {
	return f.createFn(ctx, req)
}

func (f *fakeService) GetByID(ctx context.Context, id string) (Tenant, error) {
	if f.getFn == nil {
		return Tenant{}, nil
	}
	return f.getFn(ctx, id)
}

func (f *fakeService) List(ctx context.Context, limit int32) ([]Tenant, error) {
	if f.listFn == nil {
		return nil, nil
	}
	return f.listFn(ctx, limit)
}

func TestHandlerCreateSuccess(t *testing.T) {
	now := time.Now().UTC()
	h := NewHandler(&fakeService{createFn: func(ctx context.Context, req createTenantRequest) (Tenant, error) {
		return Tenant{ID: "tenant_1", Slug: "acme", Name: "Acme", Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewBufferString(`{"slug":"acme","name":"Acme","status":"active"}`))
	h.Create().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), `"id":"tenant_1"`) {
		t.Fatalf("unexpected response body: %s", rr.Body.String())
	}
}

func TestHandlerCreateValidationFailure(t *testing.T) {
	h := NewHandler(&fakeService{createFn: func(ctx context.Context, req createTenantRequest) (Tenant, error) {
		return Tenant{}, nil
	}})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewBufferString(`{"slug":"INVALID!","name":"Acme","status":"active"}`))
	h.Create().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandlerCreateMalformedJSON(t *testing.T) {
	h := NewHandler(&fakeService{createFn: func(ctx context.Context, req createTenantRequest) (Tenant, error) {
		return Tenant{}, nil
	}})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewBufferString(`{"slug":`))
	h.Create().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestRoutesRegistered(t *testing.T) {
	m := New()
	r := httpx.NewMux()
	if err := m.Register(r); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatalf("expected tenants route to be registered")
	}
}
