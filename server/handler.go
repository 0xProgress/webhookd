// Package server implements the HTTP listener and request handler for
// webhookd.
//
// The request handling order in Handler.ServeHTTP is mandatory and is
// specified in docs/webhookd-core.md §"Request Handling Order". Any
// deviation breaks signature verification.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/0xProgress/webhookd/output"
	"github.com/0xProgress/webhookd/providers"
)

// EventWriter is the interface a writer must satisfy to receive
// verified events. Both *output.Writer (JSONL) and *output.PrettyWriter
// (human-readable) satisfy it, so the caller chooses the output mode by
// choosing which writer to pass to NewHandler.
type EventWriter interface {
	Write(*output.Event) error
}

// Handler serves webhook requests for one provider.
//
// The provider name is fixed at construction — it is not extracted from
// the URL. The CLI resolves the name from the subcommand argument and
// passes it here; the mux in server.go mounts this handler at the
// configured --path. The registry lookup inside ServeHTTP is therefore
// a defensive check whose failure indicates a programming error, not a
// routing decision.
type Handler struct {
	providerName string
	out          EventWriter
	errOut       io.Writer
	maxBody      int64
}

const (
	errMethodNotAllowed      = "method not allowed"
	errUnsupportedMediaType  = "unsupported content type"
	errPayloadTooLarge       = "request body too large"
	errUnknownProvider       = "unknown provider"
	errSigVerificationFailed = "signature verification failed"
	errInternal              = "internal error"
)

// NewHandler returns a Handler bound to the named provider.
//
// out receives verified events, one call per accepted request. errOut
// receives diagnostics — never event data. In production, out is
// os.Stdout wrapped by output.Writer or output.PrettyWriter, and errOut
// is os.Stderr.
//
// maxBody caps the request body in bytes. It is enforced during the
// read by http.MaxBytesReader, so an oversized body is never fully
// buffered.
func NewHandler(providerName string, out EventWriter, errOut io.Writer, maxBody int64) *Handler {
	if errOut == nil {
		errOut = io.Discard
	}
	return &Handler{
		providerName: providerName,
		out:          out,
		errOut:       errOut,
		maxBody:      maxBody,
	}
}

// ServeHTTP implements the mandatory request handling order from
// docs/webhookd-core.md, steps 1 through 14.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Step 1 — method check.
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errMethodNotAllowed)
		return
	}

	// Step 2 — content-type check. A charset suffix is permitted, so
	// the check is a prefix match rather than an equality check.
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, errUnsupportedMediaType)
		return
	}

	// Steps 3 and 4 — wrap the body in a size-limited reader, then read
	// it entirely. The wrapper returns *http.MaxBytesError during the
	// read when the limit is exceeded; that error is what drives the
	// 413 response. Any other read error is a 500.
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBody)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, errPayloadTooLarge)
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	// Step 5 — provider lookup. See the Handler doc comment: this is
	// defensive, and failure means the CLI accepted a name it should
	// not have.
	provider, ok := providers.Get(h.providerName)
	if !ok {
		writeError(w, http.StatusNotFound, errUnknownProvider)
		return
	}

	// Steps 6 and 7 — verify. The error string from the provider is
	// written verbatim to stderr in the format specified by
	// docs/webhookd-core.md §"Diagnostic formats". Nothing is written
	// to the event stream on a failed verification.
	if err := provider.Verify(r, rawBody); err != nil {
		_, _ = fmt.Fprintf(h.errOut, "webhookd: %s — %s\n", err.Error(), remoteIP(r))
		writeError(w, http.StatusUnauthorized, errSigVerificationFailed)
		return
	}

	// Steps 8, 9, and 10 — extract event metadata. These are called
	// after verification by design: nothing a provider returns from
	// these methods is trusted until the signature has been accepted.
	eventType := provider.EventType(r, rawBody)
	deliveryID := provider.DeliveryID(r)
	eventID := provider.EventID(r, rawBody)

	// Step 11 — validate that the body is syntactically valid JSON.
	// This runs after verification, so a hostile body is rejected at
	// step 7 before ever reaching here. The check exists to catch the
	// case of a valid signature over a body that is not JSON at all,
	// which is a provider-integration error, not an attack.
	if !json.Valid(rawBody) {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	// Step 12 — build the normalized Event. Payload holds the raw
	// request bytes as json.RawMessage so that key order, duplicate
	// keys, and numeric precision survive to the consumer.
	event := &output.Event{
		Provider:   h.providerName,
		Verified:   true,
		Event:      eventType,
		ID:         eventID,
		DeliveryID: deliveryID,
		ReceivedAt: time.Now().UTC().Format(time.RFC3339),
		Payload:    json.RawMessage(rawBody),
	}

	// Step 13 — write one JSONL line to the event stream. A write
	// failure is a diagnostic, not a data-line failure: nothing was
	// written to the event stream, and the client learns the request
	// was not recorded.
	if h.out == nil {
		_, _ = fmt.Fprintf(h.errOut, "webhookd: write event: nil EventWriter\n")
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := h.out.Write(event); err != nil {
		_, _ = fmt.Fprintf(h.errOut, "webhookd: write event: %v\n", err)
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	// Step 14 — respond.
	writeOK(w)
}

// remoteIP returns r.RemoteAddr with the port stripped. If the address
// is not in host:port form, the raw value is returned unchanged so the
// diagnostic line is never truncated.
//
// X-Forwarded-For is deliberately not consulted: it is attacker-
// controlled unless webhookd is behind a trusted proxy, and webhookd
// makes no assumption that it is.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeOK writes the 200 response body.
func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// writeError writes a JSON error response. The message is a fixed
// string chosen by the caller; nothing from the request is interpolated
// into it.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeJSON writes a JSON body with the given status. The values passed
// by this file are always marshalable; a marshal failure is treated as
// a 500 with a plain-text body rather than a panic.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
