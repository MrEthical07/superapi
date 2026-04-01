package errors

import "errors"

// Code is a stable machine-readable API error identifier.
type Code string

const (
	// CodeInternal represents unexpected internal failures.
	CodeInternal Code = "internal_error"
	// CodeBadRequest represents invalid client input.
	CodeBadRequest Code = "bad_request"
	// CodeUnsupportedMedia represents unsupported request content type.
	CodeUnsupportedMedia Code = "unsupported_media_type"
	// CodeNotFound represents missing resources.
	CodeNotFound Code = "not_found"
	// CodeMethodNotAllowed represents unsupported HTTP method.
	CodeMethodNotAllowed Code = "method_not_allowed"
	// CodeUnauthorized represents authentication failures.
	CodeUnauthorized Code = "unauthorized"
	// CodeForbidden represents authorization failures.
	CodeForbidden Code = "forbidden"
	// CodeTooManyRequests represents rate limit exhaustion.
	CodeTooManyRequests Code = "too_many_requests"
	// CodeConflict represents state conflicts.
	CodeConflict Code = "conflict"
	// CodeTimeout represents request timeout failures.
	CodeTimeout Code = "timeout"
	// CodeDependencyFailure represents unavailable dependency failures.
	CodeDependencyFailure Code = "dependency_unavailable"
)

// AppError is the typed error model used across handlers and policies.
type AppError struct {
	// Code is stable machine-readable identifier.
	Code Code
	// Message is client-facing summary.
	Message string
	// StatusCode is HTTP status to write for this error.
	StatusCode int
	// Details is optional structured payload.
	Details any
	// Cause stores underlying internal error.
	Cause error
}

// Error implements error interface.
func (e *AppError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// Unwrap returns underlying cause for errors.Is/errors.As support.
func (e *AppError) Unwrap() error { return e.Cause }

// New constructs an AppError with code, status, and message.
func New(code Code, status int, msg string) *AppError {
	return &AppError{
		Code:       code,
		StatusCode: status,
		Message:    msg,
	}
}

// WithDetails attaches structured details payload to AppError.
func WithDetails(err *AppError, details any) *AppError {
	if err == nil {
		return nil
	}
	err.Details = details
	return err
}

// WithCause attaches underlying cause to AppError.
func WithCause(err *AppError, cause error) *AppError {
	if err == nil {
		return nil
	}
	err.Cause = cause
	return err
}

// AsAppError extracts AppError from wrapped error chain.
func AsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
