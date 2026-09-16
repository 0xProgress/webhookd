// Package output defines the normalized Event struct that every
// provider's output is mapped to, and the writers that emit it.
package output

import "encoding/json"

// Event is the normalized form of a single verified webhook.
//
// Every provider produces this exact shape on stdout, one JSON object
// per line. A consumer that parses one webhookd event can parse all of
// them, regardless of which provider produced it. This is what makes
// `webhookd | jq` work without a provider-specific filter.
type Event struct {
	// Provider is the lowercase provider name, e.g. "stripe".
	Provider string `json:"provider"`

	// Verified is always true on stdout. Failed verification never
	// reaches output; a 401 is returned to the sender and a line is
	// written to stderr instead. The field exists so consumers can
	// rely on a stable schema and so the semantic meaning of the line
	// is self-describing: the presence of the line is the assertion
	// that verification succeeded.
	Verified bool `json:"verified"`

	// Event is the provider-specific event type string, or "" if the
	// provider does not supply one.
	Event string `json:"event"`

	// ID is the event's own ID, or "" if the provider does not supply
	// one.
	ID string `json:"id"`

	// DeliveryID is the delivery or request ID, or "" if the provider
	// does not supply one.
	DeliveryID string `json:"delivery_id"`

	// ReceivedAt is the time the core received the request, formatted
	// as RFC 3339 UTC (e.g. "2026-09-15T19:42:13Z").
	//
	// This is not the time the provider generated the event. Provider
	// timestamps, when present, live inside Payload. The distinction
	// matters when the two are far apart, which is usually a sign of
	// replay or clock skew.
	ReceivedAt string `json:"received_at"`

	// Payload is the JSON body of the request.
	//
	// Held as json.RawMessage so the bytes that Verify() saw are the
	// bytes the consumer sees. Decoding into map[string]any would
	// re-sort keys, collapse duplicate keys, and convert every number
	// to float64 — all of which are reshape operations the output
	// contract forbids, and the float64 conversion silently loses
	// precision on integers above 2^53, which includes real payment
	// amounts.
	//
	// encoding/json compacts a RawMessage when marshaling, so any
	// whitespace in the original body is dropped and the value is
	// emitted on one line. That is the only permitted change: the
	// structure and the numbers survive untouched.
	Payload json.RawMessage `json:"payload"`
}
