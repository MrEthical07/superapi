package httpx

import (
	"net/http"
	"reflect"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// NoBody marks request handlers that do not accept a JSON request payload.
type NoBody struct{}

// HandlerFunc is the unified typed handler signature used by Adapter.
type HandlerFunc[Req any, Resp any] func(ctx *Context, req Req) (Resp, error)

// Result allows handlers to control envelope status and OK flag explicitly.
type Result[T any] struct {
	// Status sets HTTP status code. Defaults to 200 when zero.
	Status int
	// OK overrides envelope ok flag when set.
	OK *bool
	// Data is the response payload written to envelope data.
	Data T
}

// HTTPStatus resolves default status when Status is not explicitly set.
func (r Result[T]) HTTPStatus() int {
	if r.Status == 0 {
		return http.StatusOK
	}
	return r.Status
}

// HTTPOK resolves envelope ok flag from explicit value or status code.
func (r Result[T]) HTTPOK() bool {
	if r.OK != nil {
		return *r.OK
	}
	return r.HTTPStatus() < http.StatusBadRequest
}

// HTTPData returns response payload for envelope serialization.
func (r Result[T]) HTTPData() any {
	return r.Data
}

// BoolPtr returns a bool pointer for optional envelope fields.
//
// Usage:
//
//	return httpx.Result[T]{OK: httpx.BoolPtr(true), Data: value}, nil
func BoolPtr(v bool) *bool {
	return &v
}

// Bool returns a bool pointer for optional envelope fields.
// Deprecated: use BoolPtr for clearer intent.
func Bool(v bool) *bool {
	return BoolPtr(v)
}

type unifiedResult interface {
	HTTPStatus() int
	HTTPOK() bool
	HTTPData() any
}

// Adapter converts a typed HandlerFunc into a standard http.Handler.
//
// Behavior:
// - Decodes and validates JSON input for non-NoBody requests
// - Writes unified response envelope through response package helpers
// - Uses request-id from context for deterministic error responses
func Adapter[Req any, Resp any](fn HandlerFunc[Req, Resp]) http.Handler {
	noBody := isNoBodyRequest[Req]()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestIDFromContext(r.Context())

		var req Req
		if !noBody {
			if err := DecodeAndValidateJSON(w, r, &req); err != nil {
				if _, isApp := apperr.AsAppError(err); isApp {
					response.Error(w, err, reqID)
					return
				}
				response.Error(w, mapDecodeError(err), reqID)
				return
			}
		}

		ctx := NewContext(r)
		resp, err := fn(ctx, req)
		if err != nil {
			response.Error(w, err, reqID)
			return
		}

		if wrapped, ok := any(resp).(unifiedResult); ok {
			response.JSON(w, wrapped.HTTPStatus(), response.Envelope{
				OK:        wrapped.HTTPOK(),
				Data:      wrapped.HTTPData(),
				RequestID: reqID,
			})
			return
		}

		response.OK(w, resp, reqID)
	})
}

func isNoBodyRequest[Req any]() bool {
	t := reflect.TypeOf((*Req)(nil)).Elem()
	if t == reflect.TypeOf(NoBody{}) {
		return true
	}
	return t.Kind() == reflect.Struct && t.NumField() == 0
}
