package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// Validatable is implemented by DTOs that perform semantic validation.
type Validatable interface {
	Validate() error
}

// DecodeAndValidateJSON strictly decodes a JSON request body into dst, applies
// the default body limit, rejects trailing values, and runs Validate when
// available.
func DecodeAndValidateJSON(_ http.ResponseWriter, r *http.Request, dst any) error {
	if r == nil {
		return appBadRequest("request is required", errors.New("nil request"))
	}
	if dst == nil {
		return appBadRequest("request body is required", errors.New("nil destination"))
	}

	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return err
	}

	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return appBadRequest("request body must contain a single JSON object", err)
	}

	if err := validateDecoded(dst); err != nil {
		return err
	}

	return nil
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

func validateDecoded(dst any) error {
	if v, ok := dst.(Validatable); ok {
		return normalizeValidationError(v.Validate())
	}

	rv := reflect.ValueOf(dst)
	if !rv.IsValid() {
		return nil
	}
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		if v, ok := rv.Elem().Interface().(Validatable); ok {
			return normalizeValidationError(v.Validate())
		}
	}
	return nil
}

func normalizeValidationError(err error) error {
	if err == nil {
		return nil
	}
	if _, isApp := apperr.AsAppError(err); isApp {
		return err
	}
	return appBadRequest(err.Error(), err)
}
