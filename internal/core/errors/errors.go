package errors

import "errors"

type Code string

const (
	CodeInternal          Code = "internal_error"
	CodeBadRequest        Code = "bad_request"
	CodeUnsupportedMedia  Code = "unsupported_media_type"
	CodeNotFound          Code = "not_found"
	CodeMethodNotAllowed  Code = "method_not_allowed"
	CodeUnauthorized      Code = "unauthorized"
	CodeForbidden         Code = "forbidden"
	CodeTooManyRequests   Code = "too_many_requests"
	CodeConflict          Code = "conflict"
	CodeTimeout           Code = "timeout"
	CodeDependencyFailure Code = "dependency_unavailable"
)

type AppError struct {
	Code       Code
	Message    string
	StatusCode int
	Details    any
	Cause      error
}

func (e *AppError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func (e *AppError) Unwrap() error { return e.Cause }

func New(code Code, status int, msg string) *AppError {
	return &AppError{
		Code:       code,
		StatusCode: status,
		Message:    msg,
	}
}

func WithDetails(err *AppError, details any) *AppError {
	if err == nil {
		return nil
	}
	err.Details = details
	return err
}

func WithCause(err *AppError, cause error) *AppError {
	if err == nil {
		return nil
	}
	err.Cause = cause
	return err
}

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
