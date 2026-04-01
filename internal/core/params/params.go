package params

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// URLParam returns chi route parameter value by key.
func URLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
