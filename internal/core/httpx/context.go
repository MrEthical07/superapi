package httpx

import (
	"context"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/netx"
	"github.com/MrEthical07/superapi/internal/core/params"
)

// Context wraps http.Request with typed convenience helpers for handlers.
type Context struct {
	request *http.Request
}

// NewContext constructs a handler context wrapper from incoming request.
func NewContext(r *http.Request) *Context {
	return &Context{request: r}
}

// Context returns request context or background context when request is nil.
func (c *Context) Context() context.Context {
	if c == nil || c.request == nil {
		return context.Background()
	}
	return c.request.Context()
}

// Request returns the underlying HTTP request.
func (c *Context) Request() *http.Request {
	if c == nil {
		return nil
	}
	return c.request
}

// Param returns trimmed route parameter value by name.
func (c *Context) Param(name string) string {
	if c == nil || c.request == nil {
		return ""
	}
	if value := strings.TrimSpace(params.URLParam(c.request, name)); value != "" {
		return value
	}
	return strings.TrimSpace(c.request.PathValue(name))
}

// Query returns trimmed query parameter value by name.
func (c *Context) Query(name string) string {
	if c == nil || c.request == nil {
		return ""
	}
	return strings.TrimSpace(c.request.URL.Query().Get(name))
}

// Header returns trimmed request header value by name.
func (c *Context) Header(name string) string {
	if c == nil || c.request == nil {
		return ""
	}
	return strings.TrimSpace(c.request.Header.Get(name))
}

// RequestID returns request id from context.
func (c *Context) RequestID() string {
	return RequestIDFromContext(c.Context())
}

// Auth returns authenticated principal context if available.
func (c *Context) Auth() (auth.AuthContext, bool) {
	return auth.FromContext(c.Context())
}

// ClientIP returns resolved client IP from context when available.
func (c *Context) ClientIP() (string, bool) {
	return netx.ClientIPFromContext(c.Context())
}
