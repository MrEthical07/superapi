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

type addressPart struct {
	Zip string `json:"zip"`
}

type nestedRequest struct {
	Name    string            `json:"name"`
	Count   int               `json:"count"`
	Address addressPart       `json:"address"`
	Tags    []addressPart     `json:"tags"`
	Extra   map[string]string `json:"extra"`
	Raw     json.RawMessage   `json:"raw"`
	Any     any               `json:"any"`
	Skipped string            `json:"-"`
	addressPart
}

type plainValidateRequest struct {
	Name string `json:"name"`
}

func (plainValidateRequest) Validate() error {
	return errors.New("json: cannot unmarshal something internal")
}

type errorEnvelope struct {
	OK    bool `json:"ok"`
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

// decodeVia runs body through Adapter, optionally with a body-size cap, and
// returns the status and decoded error envelope.
func decodeVia[Req any](t *testing.T, body string, limit int64) (int, errorEnvelope, string) {
	t.Helper()
	h := Adapter(func(_ *Context, _ Req) (map[string]string, error) {
		return map[string]string{"ok": "yes"}, nil
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if limit > 0 {
		req.Body = http.MaxBytesReader(rr, req.Body, limit)
	}
	h.ServeHTTP(rr, req)

	var env errorEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope %q: %v", rr.Body.String(), err)
	}
	return rr.Code, env, rr.Body.String()
}

// TestJSONRequestErrors pins the response code and message for every way a
// request body can be rejected. The messages are SuperAPI-owned constants; the
// test also fails if any encoding/json wording reaches the client, which is
// what changed between Go releases.
func TestJSONRequestErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		limit   int64
		message string
	}{
		{name: "unknown field", body: `{"name":"a","zzz":1}`, message: "unknown field in request body"},
		{name: "unknown field only", body: `{"zzz":1}`, message: "unknown field in request body"},
		{name: "unknown nested field", body: `{"name":"a","address":{"zip":"1","bad":2}}`, message: "unknown field in request body"},
		{name: "unknown field inside slice", body: `{"tags":[{"zip":"1"},{"nope":1}]}`, message: "unknown field in request body"},
		{name: "malformed JSON truncated", body: `{"name":`, message: "malformed JSON body"},
		{name: "malformed JSON garbage", body: `{name: 1}`, message: "malformed JSON body"},
		{name: "malformed JSON not json", body: `not json`, message: "malformed JSON body"},
		{name: "wrong type string for number", body: `{"count":"x"}`, message: "invalid JSON field type"},
		{name: "wrong type number for string", body: `{"name":1}`, message: "invalid JSON field type"},
		{name: "wrong type array for object", body: `[1,2]`, message: "invalid JSON field type"},
		{name: "trailing object", body: `{"name":"a"} {"name":"b"}`, message: "request body must contain a single JSON object"},
		{name: "trailing garbage", body: `{"name":"a"}x`, message: "request body must contain a single JSON object"},
		{name: "empty body", body: ``, message: "request body is required"},
		{name: "whitespace body", body: "  \n\t ", message: "request body is required"},
		{name: "body too large", body: `{"name":"` + strings.Repeat("a", 200) + `"}`, limit: 32, message: "request body too large"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, env, raw := decodeVia[nestedRequest](t, tc.body, tc.limit)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (%s)", status, http.StatusBadRequest, raw)
			}
			if env.OK || env.Error.Code != string(apperr.CodeBadRequest) {
				t.Fatalf("code = %q ok=%v, want bad_request (%s)", env.Error.Code, env.OK, raw)
			}
			if env.Error.Message != tc.message {
				t.Fatalf("message = %q, want %q", env.Error.Message, tc.message)
			}
			for _, leak := range []string{"json:", "unexpected EOF", "cannot unmarshal", "invalid character", "Go struct field", "Go value of type", "main."} {
				if strings.Contains(raw, leak) {
					t.Fatalf("library error text %q reached the client: %s", leak, raw)
				}
			}
		})
	}
}

func TestJSONUnknownFieldNamesTheField(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "top level", body: `{"name":"a","zzz":1}`, want: "zzz"},
		{name: "first in document order", body: `{"beta":1,"alpha":2}`, want: "beta"},
		{name: "nested", body: `{"address":{"zip":"1","bad":2}}`, want: "address.bad"},
		{name: "slice element", body: `{"tags":[{"zip":"1"},{"nope":1}]}`, want: "tags.nope"},
		{name: "map values are scanned", body: `{"extra":{"k":"v"},"oops":true}`, want: "oops"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, env, raw := decodeVia[nestedRequest](t, tc.body, 0)
			if env.Error.Message != "unknown field in request body" {
				t.Fatalf("message = %q (%s)", env.Error.Message, raw)
			}
			if got := env.Error.Details["field"]; got != tc.want {
				t.Fatalf("details.field = %v, want %q (%s)", got, tc.want, raw)
			}
		})
	}
}

func TestJSONUnknownFieldNameIsBounded(t *testing.T) {
	long := strings.Repeat("k", 5000)
	_, env, _ := decodeVia[nestedRequest](t, `{"`+long+`":1}`, 0)
	got, _ := env.Error.Details["field"].(string)
	if len(got) != maxReportedFieldPath {
		t.Fatalf("field length = %d, want %d", len(got), maxReportedFieldPath)
	}
}

// Keys encoding/json accepts must not be reported: case-insensitive matches,
// promoted embedded fields, raw and interface values, and map entries.
func TestJSONAcceptsKnownFields(t *testing.T) {
	bodies := []string{
		`{"name":"a","count":1}`,
		`{"NAME":"a","Count":2}`,
		`{"address":{"ZIP":"1"}}`,
		`{"zip":"promoted from the embedded struct"}`,
		`{"raw":{"anything":[1,2,{"x":1}]},"any":{"free":"form"}}`,
		`{"extra":{"any-key":"v"}}`,
		`{"tags":[{"zip":"1"},{"zip":"2"}]}`,
		`{}`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			h := Adapter(func(_ *Context, _ nestedRequest) (map[string]string, error) {
				return map[string]string{"ok": "yes"}, nil
			})
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d for %s: %s", rr.Code, body, rr.Body.String())
			}
		})
	}
}

func TestJSONIgnoredFieldIsUnknown(t *testing.T) {
	_, env, raw := decodeVia[nestedRequest](t, `{"Skipped":"x"}`, 0)
	if env.Error.Message != "unknown field in request body" {
		t.Fatalf("a json:\"-\" field must not be accepted: %s", raw)
	}
}

func TestJSONNonAppValidationErrorIsGeneric(t *testing.T) {
	status, env, raw := decodeVia[plainValidateRequest](t, `{"name":"a"}`, 0)
	if status != http.StatusBadRequest || env.Error.Code != "bad_request" {
		t.Fatalf("status=%d code=%q (%s)", status, env.Error.Code, raw)
	}
	if env.Error.Message != "request validation failed" {
		t.Fatalf("message = %q", env.Error.Message)
	}
	if strings.Contains(raw, "cannot unmarshal") {
		t.Fatalf("validation error text leaked: %s", raw)
	}
}

// The underlying decoder error is kept as the cause so logs still show it.
func TestJSONErrorsKeepTheCause(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"zzz":1}`))
	err := DecodeAndValidateJSON(httptest.NewRecorder(), req, &nestedRequest{})
	appErr, ok := apperr.AsAppError(err)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Cause == nil {
		t.Fatal("cause must be kept for logging")
	}
}
