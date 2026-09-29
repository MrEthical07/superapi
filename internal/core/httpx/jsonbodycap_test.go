package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
)

type capRequest struct {
	Data string `json:"data"`
}

func jsonCapHandler(t *testing.T, maxBody int64) http.Handler {
	t.Helper()
	echo := Adapter(func(_ *Context, req capRequest) (map[string]int, error) {
		return map[string]int{"len": len(req.Data)}, nil
	})
	return AssembleGlobalMiddleware(echo, config.HTTPMiddlewareConfig{MaxBodyBytes: maxBody}, testLogger(t), nil)
}

func postJSON(h http.Handler, dataBytes int) *httptest.ResponseRecorder {
	body := `{"data":"` + strings.Repeat("a", dataBytes) + `"}`
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	return rr
}

// With HTTP_MIDDLEWARE_MAX_BODY_BYTES=0 the middleware is not installed; the
// JSON decode path must still cap the body at DefaultJSONBodyLimit.
func TestDecodeAndValidateJSON_CapsBodyWhenMiddlewareDisabled(t *testing.T) {
	h := jsonCapHandler(t, 0)

	rr := postJSON(h, int(DefaultJSONBodyLimit)+1)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "request body too large") {
		t.Fatalf("oversized body: status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = postJSON(h, 1024)
	if rr.Code != http.StatusOK {
		t.Fatalf("small body: status=%d body=%s", rr.Code, rr.Body.String())
	}
}

// Calling the decoder directly (no middleware at all) is capped too.
func TestDecodeAndValidateJSON_CapsBodyWithoutMiddleware(t *testing.T) {
	rr := httptest.NewRecorder()
	body := `{"data":"` + strings.Repeat("a", int(DefaultJSONBodyLimit)) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var dst capRequest
	err := DecodeAndValidateJSON(rr, req, &dst)
	if err == nil || !strings.Contains(err.Error(), "request body too large") {
		t.Fatalf("err = %v, want request body too large", err)
	}
}

// A configured limit above the default is honored (not clamped to 1 MiB) and a
// configured limit below it still rejects.
func TestDecodeAndValidateJSON_UsesConfiguredLimit(t *testing.T) {
	big := jsonCapHandler(t, 4<<20)
	if rr := postJSON(big, 2<<20); rr.Code != http.StatusOK {
		t.Fatalf("2 MiB under a 4 MiB limit: status=%d body=%.120s", rr.Code, rr.Body.String())
	}
	if rr := postJSON(big, 5<<20); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "request body too large") {
		t.Fatalf("5 MiB over a 4 MiB limit: status=%d body=%.120s", rr.Code, rr.Body.String())
	}

	small := jsonCapHandler(t, 256)
	if rr := postJSON(small, 512); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "request body too large") {
		t.Fatalf("512 B over a 256 B limit: status=%d body=%.120s", rr.Code, rr.Body.String())
	}
}
