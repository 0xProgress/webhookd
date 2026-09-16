package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	// separatorWidth is the width of the horizontal rule drawn under
	// the provider name. Matches the width shown in the pretty-mode
	// examples in README.md and webhookd-core.md.
	separatorWidth = 38

	// fieldLabelWidth is the column width for the field labels in
	// pretty mode. The widest label, "Delivery ID:", is 12 characters;
	// 14 leaves two spaces of padding so the values line up.
	fieldLabelWidth = 14
)

// PrettyWriter emits events in a human-readable form to a single
// io.Writer.
//
// This is the destination selected by --pretty. It is intended for
// live demos and manual inspection, where the JSONL contract is not the
// point. The startup banner still goes to stderr; only the event body
// is written here.
//
// Safe for concurrent use for the same reason as Writer: the HTTP
// server dispatches each request on its own goroutine.
type PrettyWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewPrettyWriter returns a PrettyWriter that emits to w.
func NewPrettyWriter(w io.Writer) *PrettyWriter {
	if w == nil {
		w = io.Discard
	}
	return &PrettyWriter{w: w}
}

// Write emits e in the pretty format.
//
// The format is fixed by the examples in README.md and
// webhookd-core.md: provider name, a horizontal rule, a verification
// line, then Event / Delivery ID / Received as aligned label-value
// pairs, then the payload indented with two spaces.
//
// The payload is indented with json.Indent rather than re-marshaled, so
// the bytes that Verify() saw reach the terminal unchanged apart from
// the added indentation. This is the same fidelity guarantee that
// Writer provides in JSONL mode.
func (w *PrettyWriter) Write(e *Event) error {
	var buf bytes.Buffer

	fmt.Fprintln(&buf, e.Provider)
	fmt.Fprintln(&buf, strings.Repeat("─", separatorWidth))
	fmt.Fprintln(&buf, "✓ Signature verified")
	fmt.Fprintln(&buf)

	fmt.Fprintf(&buf, "%-*s%s\n", fieldLabelWidth, "Event:", e.Event)
	fmt.Fprintf(&buf, "%-*s%s\n", fieldLabelWidth, "Delivery ID:", e.DeliveryID)
	fmt.Fprintf(&buf, "%-*s%s\n", fieldLabelWidth, "Received:", e.ReceivedAt)

	fmt.Fprintln(&buf)

	var indented bytes.Buffer
	if err := json.Indent(&indented, e.Payload, "", "  "); err != nil {
		return fmt.Errorf("output: indent payload: %w", err)
	}
	buf.Write(indented.Bytes())
	fmt.Fprintln(&buf)

	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("output: write event: %w", err)
	}
	return nil
}
