package tenants

import (
	"context"
	"net/http"
	"strings"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/response"
	"github.com/MrEthical07/superapi/internal/core/tenant"
)

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Create() http.Handler {
	return httpx.JSON(h.create)
}

func (h *Handler) create(ctx context.Context, req createTenantRequest) (tenantResponse, error) {
	if h.svc == nil {
		return tenantResponse{}, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured")
	}
	tenant, err := h.svc.Create(ctx, req)
	if err != nil {
		return tenantResponse{}, err
	}
	return toTenantResponse(tenant), nil
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		response.Error(w, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	id := strings.TrimSpace(httpx.URLParam(r, "id"))
	if id == "" {
		response.Error(w, apperr.New(apperr.CodeBadRequest, 400, "id is required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	tenant, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}
	response.OK(w, toTenantResponse(tenant), httpx.RequestIDFromContext(r.Context()))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		response.Error(w, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	limit, err := parseListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}

	items, err := h.svc.List(r.Context(), limit)
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}

	out := make([]tenantResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toTenantResponse(item))
	}

	response.OK(w, listTenantsResponse{
		Items: out,
		Count: len(out),
		Limit: int(limit),
	}, httpx.RequestIDFromContext(r.Context()))
}

func (h *Handler) GetSelf(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		response.Error(w, apperr.New(apperr.CodeDependencyFailure, 503, "database is not configured"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	tenantID, ok := tenant.TenantIDFromContext(r.Context())
	if !ok {
		response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), httpx.RequestIDFromContext(r.Context()))
		return
	}

	tn, err := h.svc.GetByID(r.Context(), tenantID)
	if err != nil {
		response.Error(w, err, httpx.RequestIDFromContext(r.Context()))
		return
	}
	response.OK(w, toTenantResponse(tn), httpx.RequestIDFromContext(r.Context()))
}

func toTenantResponse(t Tenant) tenantResponse {
	return tenantResponse{
		ID:        t.ID,
		Slug:      t.Slug,
		Name:      t.Name,
		Status:    t.Status,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}
