package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/response"
)

const defaultJSONBodyLimit int64 = 1 << 20 // 1 MiB

type Validatable interface {
	Validate() error
}

type JSONHandlerFunc[Req any, Resp any] func(ctx context.Context, req Req) (Resp, error)

// JSON adapts a typed JSON request/response handler to net/http.
//
// Behavior:
//   - limits body size to 1 MiB
//   - requires exactly one JSON value
//   - disallows unknown fields (strict decode)
//   - invokes Validate() when request implements Validatable
//   - writes success via standard envelope
//   - maps errors through response.Error (AppError passthrough, internal sanitized)
func JSON[Req any, Resp any](fn JSONHandlerFunc[Req, Resp]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := RequestIDFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, defaultJSONBodyLimit)
		defer r.Body.Close()

		var req Req
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		if err := dec.Decode(&req); err != nil {
			response.Error(w, mapDecodeError(err), reqID)
			return
		}

		var extra struct{}
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			response.Error(w, appBadRequest("request body must contain a single JSON object", err), reqID)
			return
		}

		if v, ok := any(req).(Validatable); ok {
			if err := v.Validate(); err != nil {
				if _, isApp := apperr.AsAppError(err); isApp {
					response.Error(w, err, reqID)
					return
				}
				response.Error(w, appBadRequest(err.Error(), err), reqID)
				return
			}
		}

		resp, err := fn(r.Context(), req)
		if err != nil {
			response.Error(w, err, reqID)
			return
		}

		response.OK(w, resp, reqID)
	})
}

func appBadRequest(msg string, cause error) error {
	return apperr.WithCause(apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, msg), cause)
}

func mapDecodeError(err error) error {
	if errors.Is(err, io.EOF) {
		return appBadRequest("request body is required", err)
	}

	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return appBadRequest("request body too large", err)
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return appBadRequest("malformed JSON body", err)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return appBadRequest("invalid JSON field type", err)
	}

	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return appBadRequest("unknown field in request body", err)
	}

	return appBadRequest("invalid JSON body", err)
}
