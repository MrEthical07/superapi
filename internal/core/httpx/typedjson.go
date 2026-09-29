package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// Client-facing messages for request-body problems. They are SuperAPI-owned
// constants: nothing from encoding/json (whose error text changes between Go
// releases) is ever sent to a client. The underlying error stays attached as
// the AppError cause for logs.
const (
	msgBodyRequired  = "request body is required"
	msgBodyTooLarge  = "request body too large"
	msgBodyMalformed = "malformed JSON body"
	msgFieldType     = "invalid JSON field type"
	msgUnknownField  = "unknown field in request body"
	msgSingleObject  = "request body must contain a single JSON object"
	msgInvalidBody   = "invalid JSON body"
	msgInvalidInput  = "request validation failed"
)

// maxReportedFieldPath bounds how much of a client-supplied field name is
// echoed back in error details.
const maxReportedFieldPath = 128

// Validatable is implemented by DTOs that perform semantic validation.
type Validatable interface {
	Validate() error
}

// DecodeAndValidateJSON strictly decodes a JSON request body into dst, applies
// the default body limit, rejects trailing values, and runs Validate when
// available.
//
// Every error it returns is an *apperr.AppError with a stable client-facing
// message. A Validate error that is not already an AppError is replaced by a
// generic message (write DTO validation errors with apperr.New to control the
// text).
func DecodeAndValidateJSON(_ http.ResponseWriter, r *http.Request, dst any) error {
	if r == nil {
		return appBadRequest("request is required", errors.New("nil request"))
	}
	if dst == nil {
		return appBadRequest(msgBodyRequired, errors.New("nil destination"))
	}
	if r.Body == nil {
		return appBadRequest(msgBodyRequired, io.EOF)
	}
	defer r.Body.Close()

	// The body is read in full (it is already capped by the MaxBodyBytes
	// middleware) so an unknown field can be located structurally instead of
	// by parsing the decoder's error text.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return mapDecodeError(err, nil, dst)
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return mapDecodeError(err, body, dst)
	}

	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return appBadRequest(msgSingleObject, err)
	}

	return validateDecoded(dst)
}

func appBadRequest(msg string, cause error) *apperr.AppError {
	return apperr.WithCause(apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, msg), cause)
}

// mapDecodeError turns a decode failure into a stable AppError. Typed and
// sentinel errors are matched with errors.Is/As. encoding/json reports an
// unknown field as an untyped error, so when nothing else matches the body is
// scanned against dst's JSON field names to decide (and name) it.
func mapDecodeError(err error, body []byte, dst any) error {
	if _, isApp := apperr.AsAppError(err); isApp {
		return err
	}

	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return appBadRequest(msgBodyTooLarge, err)
	}
	if errors.Is(err, io.EOF) {
		return appBadRequest(msgBodyRequired, err)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return appBadRequest(msgBodyMalformed, err)
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return appBadRequest(msgBodyMalformed, err)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return appBadRequest(msgFieldType, err)
	}

	if body != nil {
		if path, ok := findUnknownField(body, reflect.TypeOf(dst)); ok {
			out := appBadRequest(msgUnknownField, err)
			out.Details = map[string]string{"field": truncate(path, maxReportedFieldPath)}
			return out
		}
	}

	return appBadRequest(msgInvalidBody, err)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
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

// normalizeValidationError keeps an AppError as is and replaces any other
// error with a generic message, so text from a wrapped library error can never
// reach the client. The original error stays as the cause.
func normalizeValidationError(err error) error {
	if err == nil {
		return nil
	}
	if _, isApp := apperr.AsAppError(err); isApp {
		return err
	}
	return appBadRequest(msgInvalidInput, err)
}

// --- unknown field detection ---

var unmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

// findUnknownField reports the first object key in raw that t has no JSON
// field for, as a dotted path (for example "address.zip"). It mirrors the
// matching encoding/json applies under DisallowUnknownFields: keys match the
// tag name (or field name) case-insensitively, embedded structs contribute
// their fields, and values of types with their own UnmarshalJSON, interface
// types, and maps of any key are not descended into by field name.
func findUnknownField(raw []byte, t reflect.Type) (string, bool) {
	if t == nil {
		return "", false
	}
	return scanUnknown(json.RawMessage(raw), t, "")
}

func scanUnknown(raw json.RawMessage, t reflect.Type, path string) (string, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(unmarshalerType) || t.Implements(unmarshalerType) {
		return "", false
	}

	switch t.Kind() {
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return "", false
		}
		fields := jsonFieldTypes(t)
		// Report keys in a stable order: the order they appear in the body.
		for _, key := range orderedKeys(raw) {
			ft, ok := fields[strings.ToLower(key)]
			if !ok {
				return joinPath(path, key), true
			}
			if p, found := scanUnknown(obj[key], ft, joinPath(path, key)); found {
				return p, true
			}
		}
	case reflect.Map:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return "", false
		}
		for _, key := range orderedKeys(raw) {
			if p, found := scanUnknown(obj[key], t.Elem(), joinPath(path, key)); found {
				return p, true
			}
		}
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return "", false
		}
		for _, item := range items {
			if p, found := scanUnknown(item, t.Elem(), path); found {
				return p, true
			}
		}
	}
	return "", false
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// orderedKeys returns the top-level keys of a JSON object in document order.
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		key, ok := tok.(string)
		if !ok {
			return keys
		}
		keys = append(keys, key)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

// jsonFieldTypes maps the lower-cased JSON name of every settable field of
// struct type t, including promoted fields of embedded structs, to its type.
func jsonFieldTypes(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type)
	collectFields(t, out, map[reflect.Type]bool{})
	return out
}

func collectFields(t reflect.Type, out map[string]reflect.Type, seen map[reflect.Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, hasTag := f.Tag.Lookup("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")

		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectFields(ft, out, seen)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if !hasTag || name == "" {
			name = f.Name
		}
		key := strings.ToLower(name)
		if _, dup := out[key]; !dup {
			out[key] = f.Type
		}
	}
}
