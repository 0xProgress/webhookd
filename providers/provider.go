// Package providers defines the Provider interface that every webhook
// provider implements, and the registry that maps provider names to
// implementations.
//
// The core knows nothing about any specific provider. A provider is a
// leaf node that answers five questions about a request; the pipeline
// that calls it lives in the server package.
package providers

import "net/http"

// Provider is the interface every webhook provider must implement.
//
// A provider owns its signature verification scheme entirely. The core
// makes no assumptions about how any provider signs requests.
// Implementations must use constant-time comparison for all MAC
// operations.
//
// The interface is frozen at v0.1. If a provider needs something the
// interface does not provide, the provider is wrong, not the interface.
type Provider interface {
	// Name returns the lowercase provider identifier.
	//
	// This becomes the CLI subcommand and the "provider" field in
	// output. Examples: "stripe", "github", "slack", "shopify".
	//
	// Two providers cannot share a name; Register panics on a
	// duplicate.
	Name() string

	// Verify checks the request signature against rawBody.
	//
	// rawBody is the original request body bytes, unmodified. Returns
	// nil on success, a descriptive error on failure. Errors are
	// written to stderr; the request receives a 401 and nothing is
	// written to stdout.
	//
	// Verify is the security boundary. It is responsible for reading
	// the signature header, reading any timestamp header, rejecting
	// old timestamps, computing the expected MAC, and comparing in
	// constant time.
	//
	// Implementations MUST use hmac.Equal() for all MAC comparisons.
	Verify(r *http.Request, rawBody []byte) error

	// EventType extracts the event type string from the request.
	//
	// This becomes the "event" field in output. It may read a header,
	// parse the body, or both. Return an empty string if the provider
	// does not supply an event type.
	EventType(r *http.Request, rawBody []byte) string

	// DeliveryID returns a unique delivery or request ID.
	//
	// This becomes the "delivery_id" field in output. It receives
	// only the request, not the body, because delivery IDs are always
	// in headers for the providers that have them. Return an empty
	// string if absent.
	DeliveryID(r *http.Request) string

	// EventID returns the event's own ID.
	//
	// This becomes the "id" field in output. It receives the body
	// because event IDs are often in the JSON payload. Return an
	// empty string if absent.
	//
	// The distinction from DeliveryID is real: a delivery ID
	// identifies this attempt to deliver an event, and an event ID
	// identifies the event itself. Both are optional.
	EventID(r *http.Request, rawBody []byte) string
}
