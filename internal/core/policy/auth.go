package policy

import (
	"context"
	"net/http"
	"strings"

	goauth "github.com/MrEthical07/goAuth"
	goauthmiddleware "github.com/MrEthical07/goAuth/middleware"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/params"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
	"github.com/MrEthical07/superapi/internal/core/tenant"
)

func AuthRequired(engine *goauth.Engine, mode auth.Mode) Policy {
	p := authRequiredWithEngine(engine, mode)
	return annotatePolicy(p, Metadata{Type: PolicyTypeAuthRequired, Name: "AuthRequired"})
}

func RequirePerm(perms ...string) Policy {
	required := make([]string, 0, len(perms))
	for _, p := range perms {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			required = append(required, trimmed)
		}
	}
	if len(required) == 0 {
		panicInvalidRouteConfigf("%s requires at least one permission", PolicyTypeRequirePerm)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			result, hasResult := goauthmiddleware.AuthResultFromContext(r.Context())
			engine, hasEngine := goAuthEngineFromContext(r.Context())
			if !hasResult || result == nil || !hasEngine || engine == nil {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}

			for _, permission := range required {
				if !engine.HasPermission(result.Mask, permission) {
					response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "forbidden"), rid)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}

	return annotatePolicy(p, Metadata{Type: PolicyTypeRequirePerm, Name: "RequirePerm"})
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
		panicInvalidRouteConfigf("%s requires at least one permission", PolicyTypeRequireAnyPerm)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			result, hasResult := goauthmiddleware.AuthResultFromContext(r.Context())
			engine, hasEngine := goAuthEngineFromContext(r.Context())
			if !hasResult || result == nil || !hasEngine || engine == nil {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}

			for _, permission := range required {
				if engine.HasPermission(result.Mask, permission) {
					next.ServeHTTP(w, r)
					return
				}
			}

			response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "forbidden"), rid)
		})
	}

	return annotatePolicy(p, Metadata{Type: PolicyTypeRequireAnyPerm, Name: "RequireAnyPerm"})
}

type goAuthEngineContextKey struct{}

type authGuardResponseWriter struct {
	http.ResponseWriter
	unauthorized         bool
	suppressUnauthorized func() bool
}

func (w *authGuardResponseWriter) WriteHeader(statusCode int) {
	if statusCode == http.StatusUnauthorized && w.shouldSuppressUnauthorized() {
		w.unauthorized = true
		return
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *authGuardResponseWriter) Write(data []byte) (int, error) {
	if w.unauthorized && w.shouldSuppressUnauthorized() {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}

func (w *authGuardResponseWriter) shouldSuppressUnauthorized() bool {
	if w == nil || w.suppressUnauthorized == nil {
		return true
	}
	return w.suppressUnauthorized()
}

func authRequiredWithEngine(engine *goauth.Engine, mode auth.Mode) Policy {
	routeMode := goauth.ModeInherit
	switch mode {
	case auth.ModeJWTOnly:
		routeMode = goauth.ModeJWTOnly
	case auth.ModeStrict:
		routeMode = goauth.ModeStrict
	}

	guard := goauthmiddleware.Guard(engine, routeMode)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled := false
			guarded := guard(http.HandlerFunc(func(innerW http.ResponseWriter, innerR *http.Request) {
				nextCalled = true
				result, ok := goauthmiddleware.AuthResultFromContext(innerR.Context())
				if !ok || result == nil || strings.TrimSpace(result.UserID) == "" {
					rid := requestid.FromContext(innerR.Context())
					response.Error(innerW, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
					return
				}

				principal := auth.AuthContext{
					UserID:      result.UserID,
					TenantID:    result.TenantID,
					Role:        result.Role,
					Permissions: append([]string(nil), result.Permissions...),
				}

				ctx := auth.WithContext(innerR.Context(), principal)
				ctx = context.WithValue(ctx, goAuthEngineContextKey{}, engine)
				next.ServeHTTP(innerW, innerR.WithContext(ctx))
			}))

			adapter := &authGuardResponseWriter{
				ResponseWriter: w,
				suppressUnauthorized: func() bool {
					return !nextCalled
				},
			}
			guarded.ServeHTTP(adapter, r)
			if adapter.unauthorized && !nextCalled {
				rid := requestid.FromContext(r.Context())
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
			}
		})
	}
}

func goAuthEngineFromContext(ctx context.Context) (*goauth.Engine, bool) {
	engine, ok := ctx.Value(goAuthEngineContextKey{}).(*goauth.Engine)
	if !ok || engine == nil {
		return nil, false
	}
	return engine, true
}

func TenantRequired() Policy {
	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			if _, ok := auth.FromContext(r.Context()); !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}
			if err := tenant.RequireTenant(r.Context()); err != nil {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), rid)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	return annotatePolicy(p, Metadata{Type: PolicyTypeTenantRequired, Name: "TenantRequired"})
}

func TenantMatchFromPath(paramName string) Policy {
	paramName = strings.TrimSpace(paramName)
	if paramName == "" {
		panicInvalidRouteConfigf("%s requires a non-empty path parameter name", PolicyTypeTenantMatchFromPath)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}

			resourceTenant := strings.TrimSpace(params.URLParam(r, paramName))
			if resourceTenant == "" {
				response.Error(w, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, paramName+" is required"), rid)
				return
			}
			if strings.TrimSpace(principal.TenantID) == "" {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), rid)
				return
			}
			if !tenant.IsSameTenant(principal.TenantID, resourceTenant) {
				response.Error(w, apperr.New(apperr.CodeNotFound, http.StatusNotFound, "not found"), rid)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	return annotatePolicy(p, Metadata{
		Type:            PolicyTypeTenantMatchFromPath,
		Name:            "TenantMatchFromPath",
		TenantPathParam: paramName,
	})
}
