package httpx

import (
	"net/http"
	"reflect"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/response"
)

type NoBody struct{}

type HandlerFunc[Req any, Resp any] func(ctx *Context, req Req) (Resp, error)

type Result[T any] struct {
	Status int
	OK     *bool
	Data   T
}

func (r Result[T]) HTTPStatus() int {
	if r.Status == 0 {
		return http.StatusOK
	}
	return r.Status
}

func (r Result[T]) HTTPOK() bool {
	if r.OK != nil {
		return *r.OK
	}
	return r.HTTPStatus() < http.StatusBadRequest
}

func (r Result[T]) HTTPData() any {
	return r.Data
}

func Bool(v bool) *bool {
	return &v
}

type unifiedResult interface {
	HTTPStatus() int
	HTTPOK() bool
	HTTPData() any
}

func Adapter[Req any, Resp any](fn HandlerFunc[Req, Resp]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestIDFromContext(r.Context())

		var req Req
		if !isNoBodyRequest[Req]() {
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
