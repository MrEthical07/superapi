package auth

import "context"

type principalKey struct{}

// AuthContext represents authenticated principal data attached to request context.
type AuthContext struct {
	// UserID is the canonical authenticated user identifier.
	UserID string `json:"user_id"`
	// Role is the resolved role name for RBAC checks.
	Role string `json:"role,omitempty"`
	// Permissions is the resolved permission set for RBAC checks.
	Permissions []string `json:"permissions,omitempty"`
	// Attributes carries values optional features attach to the principal
	// (see policy.AuthExtension). Read one with Attribute.
	Attributes []Attribute `json:"attributes,omitempty"`
}

// Attribute is one feature-owned key/value on the principal.
type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Attribute returns the value of the named principal attribute, or "" when no
// feature set it.
func (a AuthContext) Attribute(key string) string {
	for _, attr := range a.Attributes {
		if attr.Key == key {
			return attr.Value
		}
	}
	return ""
}

// WithContext stores AuthContext on a request context.
func WithContext(ctx context.Context, principal AuthContext) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// FromContext reads AuthContext from request context.
func FromContext(ctx context.Context) (AuthContext, bool) {
	v, ok := ctx.Value(principalKey{}).(AuthContext)
	if !ok {
		return AuthContext{}, false
	}
	return v, true
}
