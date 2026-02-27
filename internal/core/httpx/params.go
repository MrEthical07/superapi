package httpx

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/params"
)

func URLParam(r *http.Request, key string) string {
	return params.URLParam(r, key)
}
