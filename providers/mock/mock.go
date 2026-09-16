// Package mock provides a reference implementation of the Provider
// interface.
//
// It exists for two purposes: as a complete, working example that
// contributors can read and copy, and as a test double for the server
// pipeline. It is not intended for production use — its signature check
// is a literal string comparison against a well-known sentinel value,
// not a cryptographic MAC.
package mock

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/0xProgress/webhookd/providers"
)

func init() {
	providers.Register(&Provider{})
}

// Provider is the mock webhook provider.
//
// It accepts any request whose X-Mock-Signature header is the literal
// string "valid" and rejects all others. Event type and event ID are
// read from the top-level "type" and "id" fields of the JSON body.
type Provider struct{}

// Name returns the provider identifier, used as the CLI subcommand and
// the "provider" field in output.
func (p *Provider) Name() string {
	return "mock"
}

// Verify accepts the request if X-Mock-Signature is "valid" and rejects
// all others.
//
// This is not a MAC check and does not use hmac.Equal. There is no
// secret: the header is compared against a fixed sentinel value that is
// public knowledge. The rule requiring hmac.Equal applies to MAC
// comparisons, where the comparison outcome could leak information
// about a secret. Here there is no secret to leak, and a timing-safe
// comparison would protect nothing.
func (p *Provider) Verify(r *http.Request, rawBody []byte) error {
	sig := r.Header.Get("X-Mock-Signature")
	if sig == "" {
		return errors.New("mock: missing X-Mock-Signature header")
	}
	if sig != "valid" {
		return errors.New("mock: signature mismatch")
	}
	return nil
}

// EventType returns the value of the top-level "type" field in the
// body, or "" if the body is not valid JSON or the field is absent or
// not a string.
func (p *Provider) EventType(r *http.Request, rawBody []byte) string {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return ""
	}
	return envelope.Type
}

// DeliveryID is not supplied by the mock provider. Always returns "".
func (p *Provider) DeliveryID(r *http.Request) string {
	return ""
}

// EventID returns the value of the top-level "id" field in the body, or
// "" if the body is not valid JSON or the field is absent or not a
// string.
func (p *Provider) EventID(r *http.Request, rawBody []byte) string {
	var envelope struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return ""
	}
	return envelope.ID
}
