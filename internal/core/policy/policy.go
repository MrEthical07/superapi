package policy

import "net/http"

type Policy func(http.Handler) http.Handler

func Chain(h http.Handler, policies ...Policy) http.Handler {
	for i := len(policies) - 1; i >= 0; i-- {
		if policies[i] == nil {
			continue
		}
		h = policies[i](h)
	}
	return h
}

func Noop() Policy {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}

func RequireJSON() Policy {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requiresJSONBody(r) {
				next.ServeHTTP(w, r)
				return
			}

			if !isJSONContentType(r.Header.Get("Content-Type")) {
				http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func WithHeader(key, value string) Policy {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(key, value)
			next.ServeHTTP(w, r)
		})
	}
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
	if contentType == "application/json" {
		return true
	}
	if len(contentType) > len("application/json") && contentType[:len("application/json")] == "application/json" {
		return true
	}
	return false
}
