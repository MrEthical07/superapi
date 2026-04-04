package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestTimeoutReturnsGatewayTimeoutEnvelope(t *testing.T) {
	h := RequestID(RequestTimeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusGatewayTimeout)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if ok, _ := body["ok"].(bool); ok {
		t.Fatalf("ok = true, want false")
	}

	errorObj, _ := body["error"].(map[string]any)
	if got, _ := errorObj["code"].(string); got != "timeout" {
		t.Fatalf("error.code = %q, want %q", got, "timeout")
	}
	if got, _ := errorObj["message"].(string); got != "request timed out" {
		t.Fatalf("error.message = %q, want %q", got, "request timed out")
	}

	if got, _ := body["request_id"].(string); got == "" {
		t.Fatalf("expected request_id in timeout response")
	}
}

func TestRequestTimeoutAllowsFastRequest(t *testing.T) {
	h := RequestID(RequestTimeout(100 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRequestTimeoutBypassesSSERequests(t *testing.T) {
	h := RequestTimeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRequestTimeoutBypassesWebsocketUpgrade(t *testing.T) {
	h := RequestTimeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusSwitchingProtocols)
	}
}
