package httpx

import (
	"context"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/netx"
	"github.com/MrEthical07/superapi/internal/core/params"
)

type Context struct {
	request *http.Request
}

func NewContext(r *http.Request) *Context {
	return &Context{request: r}
}

func (c *Context) Context() context.Context {
	if c == nil || c.request == nil {
		return context.Background()
	}
	return c.request.Context()
}

func (c *Context) Request() *http.Request {
	if c == nil {
		return nil
	}
	return c.request
}

func (c *Context) Param(name string) string {
	if c == nil || c.request == nil {
		return ""
	}
	if value := strings.TrimSpace(params.URLParam(c.request, name)); value != "" {
		return value
	}
	return strings.TrimSpace(c.request.PathValue(name))
}

func (c *Context) Query(name string) string {
	if c == nil || c.request == nil {
		return ""
	}
	return strings.TrimSpace(c.request.URL.Query().Get(name))
}

func (c *Context) Header(name string) string {
	if c == nil || c.request == nil {
		return ""
	}
	return strings.TrimSpace(c.request.Header.Get(name))
}

func (c *Context) RequestID() string {
	return RequestIDFromContext(c.Context())
}

func (c *Context) Auth() (auth.AuthContext, bool) {
	return auth.FromContext(c.Context())
}

func (c *Context) ClientIP() (string, bool) {
	return netx.ClientIPFromContext(c.Context())
}
