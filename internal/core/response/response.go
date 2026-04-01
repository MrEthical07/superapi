package response

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// ErrorBody is the API error payload embedded in response envelope.
type ErrorBody struct {
	// Code is stable machine-readable error code.
	Code string `json:"code"`
	// Message is client-facing error summary.
	Message string `json:"message"`
	// Details contains optional structured diagnostics safe for clients.
	Details any `json:"details,omitempty"`
}

// Envelope is the standard API response shape for all endpoints.
type Envelope struct {
	// OK indicates whether request completed successfully.
	OK bool `json:"ok"`
	// Data contains success payload.
	Data any `json:"data,omitempty"`
	// Error contains error payload for failed responses.
	Error *ErrorBody `json:"error,omitempty"`
	// RequestID propagates request correlation identifier.
	RequestID string `json:"request_id,omitempty"`
	// Meta contains optional non-primary payload metadata.
	Meta any `json:"meta,omitempty"`
}

// JSON writes a response envelope as JSON with explicit HTTP status code.
func JSON(w http.ResponseWriter, status int, payload Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(payload) // best effort; status already written
}

// OK writes a 200 success envelope.
func OK(w http.ResponseWriter, data any, requestID string) {
	JSON(w, http.StatusOK, Envelope{
		OK:        true,
		Data:      data,
		RequestID: requestID,
	})
}

// Created writes a 201 success envelope.
func Created(w http.ResponseWriter, data any, requestID string) {
	JSON(w, http.StatusCreated, Envelope{
		OK:        true,
		Data:      data,
		RequestID: requestID,
	})
}

// Error writes sanitized error envelope based on typed app errors.
//
// Behavior:
// - Deadline exceeded maps to timeout response
// - AppError maps to configured status/code/message/details
// - Unknown errors map to generic internal error response
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
