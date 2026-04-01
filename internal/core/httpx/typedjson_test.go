package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

type echoRequest struct {
	Name string `json:"name"`
}

func (r echoRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "name is required")
	}
	return nil
}

func TestJSON_Success(t *testing.T) {
	h := Adapter(func(ctx *Context, req echoRequest) (map[string]string, error) {
		return map[string]string{"name": req.Name}, nil
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"alice"}`))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["ok"] != true {
		t.Fatalf("ok = %v, want true", got["ok"])
	}
}

func TestJSON_MalformedJSON(t *testing.T) {
	h := Adapter(func(ctx *Context, req echoRequest) (map[string]string, error) {
		return map[string]string{"name": req.Name}, nil
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), `"code":"bad_request"`) {
		t.Fatalf("response missing bad_request code: %s", rr.Body.String())
	}
}

func TestJSON_ValidationFailure(t *testing.T) {
	h := Adapter(func(ctx *Context, req echoRequest) (map[string]string, error) {
		return map[string]string{"name": req.Name}, nil
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":""}`))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "name is required") {
		t.Fatalf("validation message missing: %s", rr.Body.String())
	}
}

func TestJSON_AppErrorPassthrough(t *testing.T) {
	h := Adapter(func(ctx *Context, req echoRequest) (map[string]string, error) {
		return nil, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "business rule violated")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"alice"}`))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "business rule violated") {
		t.Fatalf("app error message missing: %s", rr.Body.String())
	}
}

func TestJSON_InternalErrorSanitized(t *testing.T) {
	h := Adapter(func(ctx *Context, req echoRequest) (map[string]string, error) {
		return nil, errors.New("database connection refused")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"alice"}`))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rr.Body.String(), "database connection refused") {
		t.Fatalf("internal error leaked to client: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("response missing internal_error code: %s", rr.Body.String())
	}
}

func TestJSONWithRequest_ExposesRequest(t *testing.T) {
	h := Adapter(func(ctx *Context, req echoRequest) (map[string]string, error) {
		return map[string]string{
			"id":   ctx.Param("id"),
			"name": req.Name,
		}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/projects/abc", strings.NewReader(`{"name":"alice"}`))
	req.SetPathValue("id", "abc")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), `"id":"abc"`) {
		t.Fatalf("expected path value in response: %s", rr.Body.String())
	}
}

func TestAdapter_NoBodyRequest(t *testing.T) {
	h := Adapter(func(ctx *Context, req NoBody) (map[string]string, error) {
		return map[string]string{"status": "ok"}, nil
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestDecodeAndValidateJSON_AppErrorPassthrough(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":""}`))
	rr := httptest.NewRecorder()

	var dst echoRequest
	err := DecodeAndValidateJSON(rr, req, &dst)
	if err == nil {
		t.Fatal("expected validation error")
	}
	appErr, ok := apperr.AsAppError(err)
	if !ok {
		t.Fatalf("expected app error, got %T", err)
	}
	if appErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", appErr.StatusCode, http.StatusBadRequest)
	}
}
