package response

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type Envelope struct {
	OK        bool       `json:"ok"`
	Data      any        `json:"data,omitempty"`
	Error     *ErrorBody `json:"error,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	Meta      any        `json:"meta,omitempty"`
}

func JSON(w http.ResponseWriter, status int, payload Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(payload) // best effort; status already written
}

func OK(w http.ResponseWriter, data any, requestID string) {
	JSON(w, http.StatusOK, Envelope{
		OK:        true,
		Data:      data,
		RequestID: requestID,
	})
}

func Created(w http.ResponseWriter, data any, requestID string) {
	JSON(w, http.StatusCreated, Envelope{
		OK:        true,
		Data:      data,
		RequestID: requestID,
	})
}

func Error(w http.ResponseWriter, err error, requestID string) {
	if errors.Is(err, context.DeadlineExceeded) {
		JSON(w, http.StatusGatewayTimeout, Envelope{
			OK: false,
			Error: &ErrorBody{
				Code:    string(apperr.CodeTimeout),
				Message: "request timed out",
			},
			RequestID: requestID,
		})
		return
	}

	if ae, ok := apperr.AsAppError(err); ok {
		JSON(w, ae.StatusCode, Envelope{
			OK: false,
			Error: &ErrorBody{
				Code:    string(ae.Code),
				Message: ae.Message,
				Details: ae.Details,
			},
			RequestID: requestID,
		})
		return
	}

	JSON(w, http.StatusInternalServerError, Envelope{
		OK: false,
		Error: &ErrorBody{
			Code:    string(apperr.CodeInternal),
			Message: "internal server error",
		},
		RequestID: requestID,
	})
}
