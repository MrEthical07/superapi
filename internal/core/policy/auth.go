package policy

import (
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/params"
	"github.com/MrEthical07/superapi/internal/core/response"
	"github.com/MrEthical07/superapi/internal/core/tenant"
)

func AuthRequired(provider auth.Provider, mode auth.Mode) Policy {
	if provider == nil {
		provider = auth.NewDisabledProvider()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), "")
				return
			}

			principal, err := provider.Authenticate(r.Context(), token, mode)
			if err != nil {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), "")
				return
			}

			ctx := auth.WithContext(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireRole(roles ...string) Policy {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		trimmed := strings.TrimSpace(role)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), "")
				return
			}
			if _, has := allowed[principal.Role]; !has {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "forbidden"), "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequirePerm(perms ...string) Policy {
	required := make([]string, 0, len(perms))
	for _, p := range perms {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			required = append(required, trimmed)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), "")
				return
			}

			available := make(map[string]struct{}, len(principal.Permissions))
			for _, permission := range principal.Permissions {
				available[permission] = struct{}{}
			}

			for _, permission := range required {
				if _, has := available[permission]; !has {
					response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "forbidden"), "")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RequireAnyPerm(perms ...string) Policy {
	required := make([]string, 0, len(perms))
	for _, p := range perms {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			required = append(required, trimmed)
		}
	}
	if len(required) == 0 {
		return Noop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), "")
				return
			}

			available := make(map[string]struct{}, len(principal.Permissions))
			for _, permission := range principal.Permissions {
				available[permission] = struct{}{}
			}

			for _, permission := range required {
				if _, has := available[permission]; has {
					next.ServeHTTP(w, r)
					return
				}
			}

			response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "forbidden"), "")
		})
	}
}

func TenantRequired() Policy {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.FromContext(r.Context()); !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), "")
				return
			}
			if err := tenant.RequireTenant(r.Context()); err != nil {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func TenantMatchFromPath(paramName string) Policy {
	paramName = strings.TrimSpace(paramName)
	if paramName == "" {
		paramName = "id"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), "")
				return
			}

			resourceTenant := strings.TrimSpace(params.URLParam(r, paramName))
			if resourceTenant == "" {
				response.Error(w, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, paramName+" is required"), "")
				return
			}
			if strings.TrimSpace(principal.TenantID) == "" {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), "")
				return
			}
			if !tenant.IsSameTenant(principal.TenantID, resourceTenant) {
				response.Error(w, apperr.New(apperr.CodeNotFound, http.StatusNotFound, "not found"), "")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(header string) (string, bool) {
	if header == "" {
		return "", false
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	if parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
