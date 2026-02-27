package auth

import "context"

type principalKey struct{}

type AuthContext struct {
	UserID      string   `json:"user_id"`
	TenantID    string   `json:"tenant_id,omitempty"`
	Role        string   `json:"role,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

func WithContext(ctx context.Context, principal AuthContext) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func FromContext(ctx context.Context) (AuthContext, bool) {
	v, ok := ctx.Value(principalKey{}).(AuthContext)
	if !ok {
		return AuthContext{}, false
	}
	return v, true
}
