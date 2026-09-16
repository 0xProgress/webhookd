package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Writer emits events as JSONL to a single io.Writer.
//
// The destination is provided by the caller rather than fixed to
// os.Stdout so that tests can capture the raw bytes and assert on
// them. Production code passes os.Stdout.
//
// Safe for concurrent use. The HTTP server handles requests in
// separate goroutines, so two handlers can reach Write at the same
// time; without the mutex their byte sequences could interleave on
// the underlying writer and produce invalid JSONL.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

// NewWriter returns a Writer that emits to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write emits one JSONL line for e.
//
// The line is exactly one compact JSON object followed by '\n'. HTML
// escaping is disabled so that '<', '>', and '&' in the payload are
// emitted as-is rather than rewritten to \u003c, \u003e, and \u0026;
// the default encoding/json behaviour would silently alter bytes the
// output contract says are passed through unmodified.
func (w *Writer) Write(e *Event) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return fmt.Errorf("output: marshal event: %w", err)
	}
	// Encoder.Encode terminates the value with '\n'. Nothing else may
	// be appended: the JSONL contract is one line per event.

	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("output: write event: %w", err)
	}
	return nil
}
