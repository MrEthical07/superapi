package policy

import (
	"bufio"
	"context"
	"net"
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

// AuthRequired enforces authentication on a route.
//
// It validates bearer token claims using the configured goAuth engine and
// injects auth.AuthContext into request context for downstream policies.
//
// Behavior:
// - Returns 401 if token is missing or invalid
// - Uses selected mode (jwt_only, hybrid, strict)
//
// Usage:
//
//	r.Handle(http.MethodGet, "/api/v1/system/whoami", httpx.Adapter(handler),
//	    policy.AuthRequired(engine, mode),
//	)
//
// Notes:
// - Place before RBAC and tenant policies
// - Requires a non-nil engine when route is expected to be protected
func AuthRequired(engine *goauth.Engine, mode auth.Mode) Policy {
	p := authRequiredWithEngine(engine, mode)
	return annotatePolicy(p, Metadata{Type: PolicyTypeAuthRequired, Name: "AuthRequired"})
}

// RequirePerm enforces all-of permission checks for authenticated users.
//
// Behavior:
// - Returns 401 if auth context is missing
// - Returns 403 if any required permission is missing
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

// RequireAnyPerm enforces any-of permission checks for authenticated users.
//
// Behavior:
// - Returns 401 if auth context is missing
// - Returns 403 if none of the permissions match
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

// WriteHeader suppresses premature 401 writes when guard short-circuits.
func (w *authGuardResponseWriter) WriteHeader(statusCode int) {
	if statusCode == http.StatusUnauthorized && w.shouldSuppressUnauthorized() {
		w.unauthorized = true
		return
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write discards body bytes for suppressed unauthorized responses.
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

// SetRoutePattern forwards route-pattern propagation when supported.
func (w *authGuardResponseWriter) SetRoutePattern(pattern string) {
	if setter, ok := w.ResponseWriter.(interface{ SetRoutePattern(string) }); ok {
		setter.SetRoutePattern(pattern)
	}
}

// Flush forwards flush capability when supported by underlying writer.
func (w *authGuardResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards connection hijack when supported.
func (w *authGuardResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// Push forwards HTTP/2 server push when supported.
func (w *authGuardResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
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

// TenantRequired ensures authenticated requests carry tenant scope.
//
// Behavior:
// - Returns 401 when authentication context is absent
// - Returns 403 when tenant scope is missing
//
// Notes:
// - Required for tenant-isolated routes
// - Place after AuthRequired
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

// TenantMatchFromPath enforces tenant isolation using a route path parameter.
//
// Behavior:
// - Returns 400 when route tenant parameter is missing
// - Returns 401 when auth context is missing
// - Returns 404 when principal tenant and route tenant mismatch
//
// Usage:
//
//	r.Handle(http.MethodGet, "/api/v1/tenants/{tenant_id}/projects", handler,
//	    policy.AuthRequired(engine, mode),
//	    policy.TenantRequired(),
//	    policy.TenantMatchFromPath("tenant_id"),
//	)
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
