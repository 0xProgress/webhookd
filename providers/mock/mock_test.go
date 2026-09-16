package mock

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/0xProgress/webhookd/providers"
)

func TestName(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "mock" {
		t.Fatalf("Name() = %q, want %q", got, "mock")
	}
}

func TestRegister(t *testing.T) {
	// The init() in mock.go should have registered the provider under
	// the name returned by Name(). If this test fails, the blank import
	// pattern is broken somewhere between mock.go and the caller.
	got, ok := providers.Get("mock")
	if !ok {
		t.Fatal("providers.Get(\"mock\") reported not found; init() did not register")
	}
	if _, isMock := got.(*Provider); !isMock {
		t.Fatalf("providers.Get(\"mock\") returned %T, want *mock.Provider", got)
	}
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name    string
		sig     string // "" means the header is omitted
		wantErr bool
	}{
		{name: "valid", sig: "valid", wantErr: false},
		{name: "wrong value", sig: "invalid", wantErr: true},
		{name: "empty value", sig: "", wantErr: true},
		{name: "case sensitive", sig: "Valid", wantErr: true},
		{name: "whitespace", sig: " valid ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"type":"test.event","id":"evt_1"}`)
			req := httptest.NewRequest("POST", "/mock", bytes.NewReader(body))
			if tt.sig != "" {
				req.Header.Set("X-Mock-Signature", tt.sig)
			}

			err := (&Provider{}).Verify(req, body)
			if tt.wantErr && err == nil {
				t.Fatal("Verify returned nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Verify returned %v, want nil", err)
			}
		})
	}
}

func TestVerifyDoesNotReadBody(t *testing.T) {
	// The mock provider's signature is entirely in the header. Body
	// contents must not affect the verification result. This mirrors
	// the pipeline invariant from the other direction: Verify sees the
	// same bytes the caller will see, but for the mock those bytes
	// carry no signature information.
	req := httptest.NewRequest("POST", "/mock", nil)
	req.Header.Set("X-Mock-Signature", "valid")

	if err := (&Provider{}).Verify(req, []byte("not json at all")); err != nil {
		t.Fatalf("Verify rejected a request with a valid header and garbage body: %v", err)
	}
}

func TestEventType(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "present", body: `{"type":"order.created"}`, want: "order.created"},
		{name: "absent", body: `{"id":"evt_1"}`, want: ""},
		{name: "not a string", body: `{"type":42}`, want: ""},
		{name: "empty body", body: ``, want: ""},
		{name: "invalid json", body: `{`, want: ""},
		{name: "top-level array", body: `["type"]`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mock", nil)
			got := (&Provider{}).EventType(req, []byte(tt.body))
			if got != tt.want {
				t.Fatalf("EventType(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestEventID(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "present", body: `{"id":"evt_abc123"}`, want: "evt_abc123"},
		{name: "absent", body: `{"type":"order.created"}`, want: ""},
		{name: "not a string", body: `{"id":123}`, want: ""},
		{name: "empty body", body: ``, want: ""},
		{name: "invalid json", body: `null`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mock", nil)
			got := (&Provider{}).EventID(req, []byte(tt.body))
			if got != tt.want {
				t.Fatalf("EventID(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestDeliveryIDIsAlwaysEmpty(t *testing.T) {
	// The mock provider does not supply a delivery ID. Even when the
	// request carries a header that looks like one, the method returns
	// the empty string — the contract is that absence is expressed as
	// "", never inferred from unrelated headers.
	req := httptest.NewRequest("POST", "/mock", nil)
	req.Header.Set("X-Mock-Delivery", "should-be-ignored")

	if got := (&Provider{}).DeliveryID(req); got != "" {
		t.Fatalf("DeliveryID() = %q, want empty string", got)
	}
}
