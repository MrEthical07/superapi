package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/MrEthical07/superapi/internal/core/policy"
)

// Router is the abstraction modules use to register routes.
// Module code depends only on this interface, never on chi directly.
type Router interface {
	Handle(method string, pattern string, h http.Handler, policies ...policy.Policy)
}

// Mux is the chi-backed Router implementation.
// It satisfies both Router (for module registration) and http.Handler (for the HTTP server).
type Mux struct {
	r chi.Router
}

// NewMux creates a production-ready router backed by chi.
func NewMux() *Mux {
	return &Mux{r: chi.NewRouter()}
}

// ServeHTTP delegates to the underlying chi router.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.r.ServeHTTP(w, r)
}

// Handle registers a handler for the given HTTP method and pattern.
// Chi provides native method routing with automatic 405 Method Not Allowed
// for known paths reached with the wrong method, and 404 Not Found for
// unknown paths.
//
// Note: chi's Method() does not return an error at registration time;
// invalid patterns will panic immediately (fail-fast), which is acceptable
// for route registration at startup.
func (m *Mux) Handle(method string, pattern string, h http.Handler, policies ...policy.Policy) {
	final := policy.Chain(h, policies...)
	m.r.Method(method, pattern, final)
}
