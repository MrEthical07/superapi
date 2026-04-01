package policy

import (
	"mime"
	"net/http"
	"strings"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// Policy wraps an HTTP handler with route-level behavior.
//
// Policies are applied during route registration and composed with Chain.
type Policy func(http.Handler) http.Handler

// Chain composes policies in registration order.
//
// Usage:
//
//	r.Handle(http.MethodGet, "/profile", handler,
//	    policy.AuthRequired(engine, mode),
//	    policy.RequirePerm("profile.read"),
//	)
func Chain(h http.Handler, policies ...Policy) http.Handler {
	for i := len(policies) - 1; i >= 0; i-- {
		if policies[i] == nil {
			continue
		}
		h = policies[i](h)
	}
	return h
}

// Noop returns a policy that forwards requests unchanged.
func Noop() Policy {
	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
	return annotatePolicy(p, Metadata{Type: PolicyTypeNoop, Name: "Noop"})
}

// RequireJSON enforces application/json content type for body-carrying requests.
//
// Behavior:
// - Validates Content-Type for POST/PUT/PATCH and payload-bearing requests
// - Returns 415 when content type is not JSON
func RequireJSON() Policy {
	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requiresJSONBody(r) {
				next.ServeHTTP(w, r)
				return
			}

			if !isJSONContentType(r.Header.Get("Content-Type")) {
				rid := requestid.FromContext(r.Context())
				response.Error(w, apperr.New(apperr.CodeUnsupportedMedia, http.StatusUnsupportedMediaType, "content type must be application/json"), rid)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
	return annotatePolicy(p, Metadata{Type: PolicyTypeRequireJSON, Name: "RequireJSON"})
}

// WithHeader injects a static response header for matched routes.
func WithHeader(key, value string) Policy {
	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(key, value)
			next.ServeHTTP(w, r)
		})
	}
	return annotatePolicy(p, Metadata{Type: PolicyTypeWithHeader, Name: "WithHeader"})
}

func requiresJSONBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return r.ContentLength > 0 || len(r.TransferEncoding) > 0
	}
}

func isJSONContentType(contentType string) bool {
	trimmed := strings.TrimSpace(contentType)
	if trimmed == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(trimmed)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}
