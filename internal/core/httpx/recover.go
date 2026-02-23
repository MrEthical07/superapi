package httpx

import (
	"log"
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/response"
)

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				rid := RequestIDFromContext(r.Context())
				log.Printf("panic recovered request_id=%s method=%s path=%s panic=%v", rid, r.Method, r.URL.Path, rec)
				response.Error(w, nil, rid)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
