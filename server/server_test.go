// Package server_test exercises the HTTP pipeline end to end through a
// real listener. It asserts on raw bytes: HTTP status, response body,
// stdout, and stderr. It never parses stdout into a struct, because
// doing so would hide unexpected extra output.
package server_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xProgress/webhookd/output"
	"github.com/0xProgress/webhookd/server"

	_ "github.com/0xProgress/webhookd/providers/mock"
)

// syncBuffer is a bytes.Buffer guarded by a mutex.
//
// The server handler writes to stdout and stderr from the server's
// goroutine while the test reads the same buffers from its own
// goroutine. Without the mutex the race detector correctly reports a
// data race on the buffer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// testServer is a running webhookd server plus the buffers its handler
// writes to.
type testServer struct {
	URL    string
	Path   string
	Stdout *syncBuffer
	Stderr *syncBuffer
}

type testServerConfig struct {
	providerName string
	maxBody      int64
	version      string
}

type testServerOption func(*testServerConfig)

func withProviderName(name string) testServerOption {
	return func(c *testServerConfig) { c.providerName = name }
}

func withMaxBody(n int64) testServerOption {
	return func(c *testServerConfig) { c.maxBody = n }
}

// newTestServer starts a Server on an ephemeral port and returns it
// along with the buffers the handler writes to. The server is shut
// down when the test ends.
func newTestServer(t *testing.T, opts ...testServerOption) *testServer {
	t.Helper()

	cfg := testServerConfig{
		providerName: "mock",
		maxBody:      2 * 1024 * 1024,
		version:      "v0.1.0",
	}
	for _, o := range opts {
		o(&cfg)
	}

	stdout := &syncBuffer{}
	stderr := &syncBuffer{}

	handler := server.NewHandler(
		cfg.providerName,
		output.NewWriter(stdout),
		stderr,
		cfg.maxBody,
	)

	path := "/" + cfg.providerName

	httpSrv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Path:    path,
		Version: cfg.version,
		Timeout: 10 * time.Second,
		Handler: handler,
		ErrOut:  nil, // suppress the banner so stderr assertions are exact
	})

	ln, err := httpSrv.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	go func() {
		_ = httpSrv.Serve(ln)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	})

	return &testServer{
		URL:    "http://" + ln.Addr().String(),
		Path:   path,
		Stdout: stdout,
		Stderr: stderr,
	}
}

// postJSON sends a POST with Content-Type application/json and the
// given signature and body. An empty sig omits the X-Mock-Signature
// header entirely.
func postJSON(t *testing.T, ts *testServer, sig, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+ts.Path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sig != "" {
		req.Header.Set("X-Mock-Signature", sig)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// drainBody reads and closes the response body.
func drainBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The em dash character used in the failed-verification diagnostic.
// It is the character the spec fixes, U+2014.
const emDash = "\u2014"

func TestVerified_JSONLOutput(t *testing.T) {
	ts := newTestServer(t)
	const body = `{"type":"order.created","id":"evt_abc123","data":{"amount":4200}}`

	resp := postJSON(t, ts, "valid", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"ok":true}` {
		t.Fatalf("response body = %q, want %q", got, `{"ok":true}`)
	}

	if ts.Stderr.Len() != 0 {
		t.Fatalf("stderr not empty: %q", ts.Stderr.String())
	}

	stdout := ts.Stdout.String()
	if n := strings.Count(stdout, "\n"); n != 1 {
		t.Fatalf("stdout has %d newlines, want exactly 1:\n%q", n, stdout)
	}

	prefix := `{"provider":"mock","verified":true,"event":"order.created","id":"evt_abc123","delivery_id":"","received_at":"`
	if !strings.HasPrefix(stdout, prefix) {
		t.Fatalf("stdout prefix mismatch:\ngot:  %q\nwant: %q...", stdout, prefix)
	}
	suffix := `","payload":` + body + "}\n"
	if !strings.HasSuffix(stdout, suffix) {
		t.Fatalf("stdout suffix mismatch:\ngot:  %q\nwant: ...%q", stdout, suffix)
	}

	// Extract the received_at value between the two known delimiters
	// and check it is RFC3339 UTC.
	rest := stdout[len(prefix):]
	idx := strings.Index(rest, `","payload":`)
	if idx < 0 {
		t.Fatal("could not find payload delimiter in stdout")
	}
	received := rest[:idx]
	if !strings.HasSuffix(received, "Z") {
		t.Fatalf("received_at %q does not end in Z", received)
	}
	if _, err := time.Parse(time.RFC3339, received); err != nil {
		t.Fatalf("received_at %q is not RFC3339: %v", received, err)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + ts.Path)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"method not allowed"}` {
		t.Fatalf("response body = %q", got)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
}

func TestUnsupportedContentType(t *testing.T) {
	ts := newTestServer(t)
	req, err := http.NewRequest(http.MethodPost, ts.URL+ts.Path, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Mock-Signature", "valid")
	// Deliberately no Content-Type header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"unsupported content type"}` {
		t.Fatalf("response body = %q", got)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
}

func TestContentTypeWithCharset(t *testing.T) {
	ts := newTestServer(t)
	req, err := http.NewRequest(http.MethodPost, ts.URL+ts.Path, strings.NewReader(`{"type":"t"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-Mock-Signature", "valid")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_ = drainBody(t, resp)
}

func TestBodyTooLarge(t *testing.T) {
	ts := newTestServer(t, withMaxBody(1024))
	// A 2KB body against a 1KB limit.
	body := `{"pad":"` + strings.Repeat("a", 2048) + `"}`
	resp := postJSON(t, ts, "valid", body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"request body too large"}` {
		t.Fatalf("response body = %q", got)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
}

func TestMissingSignature(t *testing.T) {
	ts := newTestServer(t)
	resp := postJSON(t, ts, "", `{"type":"test"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"signature verification failed"}` {
		t.Fatalf("response body = %q", got)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
	want := "webhookd: mock: missing X-Mock-Signature header " + emDash + " 127.0.0.1\n"
	if got := ts.Stderr.String(); got != want {
		t.Fatalf("stderr =\n  %q\nwant:\n  %q", got, want)
	}
}

func TestWrongSignature(t *testing.T) {
	ts := newTestServer(t)
	resp := postJSON(t, ts, "invalid", `{"type":"test"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"signature verification failed"}` {
		t.Fatalf("response body = %q", got)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
	want := "webhookd: mock: signature mismatch " + emDash + " 127.0.0.1\n"
	if got := ts.Stderr.String(); got != want {
		t.Fatalf("stderr =\n  %q\nwant:\n  %q", got, want)
	}
}

func TestMalformedJSON(t *testing.T) {
	ts := newTestServer(t)
	resp := postJSON(t, ts, "valid", `{`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"internal error"}` {
		t.Fatalf("response body = %q", got)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
}

func TestUnknownProvider(t *testing.T) {
	ts := newTestServer(t, withProviderName("nonexistent"))
	resp := postJSON(t, ts, "valid", `{"type":"test"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"unknown provider"}` {
		t.Fatalf("response body = %q", got)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
}

func TestPayloadPreserved(t *testing.T) {
	ts := newTestServer(t)
	// Key order is not alphabetical; a number exceeds 2^53; a key is
	// duplicated. All three survive only if the payload is emitted as
	// the original request bytes rather than re-serialized from a map.
	const body = `{"z":1,"a":2,"big":9007199254740993,"dup":1,"dup":2}`
	resp := postJSON(t, ts, "valid", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_ = drainBody(t, resp)

	stdout := ts.Stdout.String()
	want := `"payload":` + body + "}"
	if !strings.Contains(stdout, want) {
		t.Fatalf("payload not preserved:\ngot:  %s\nwant to contain: %s", stdout, want)
	}
}

func TestPayloadCompacted(t *testing.T) {
	ts := newTestServer(t)
	// Whitespace outside strings is dropped by the JSONL encoder; key
	// order is preserved. The output payload must be the compact form
	// of the request body.
	const body = "{\n  \"z\": 1,\n  \"a\": 2\n}"
	resp := postJSON(t, ts, "valid", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_ = drainBody(t, resp)

	stdout := ts.Stdout.String()
	want := `"payload":{"z":1,"a":2}}`
	if !strings.Contains(stdout, want) {
		t.Fatalf("payload not compacted:\ngot:  %s\nwant to contain: %s", stdout, want)
	}
	if strings.Contains(stdout, "\n  ") {
		t.Fatalf("stdout contains raw indentation from the request body:\n%s", stdout)
	}
}

func TestPayloadHTMLNotEscaped(t *testing.T) {
	ts := newTestServer(t)
	// encoding/json escapes <, >, and & by default. output.Writer
	// disables that so the payload survives with the exact bytes the
	// provider sent.
	const body = `{"msg":"<script>&</script>"}`
	resp := postJSON(t, ts, "valid", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_ = drainBody(t, resp)

	stdout := ts.Stdout.String()
	if !strings.Contains(stdout, `"payload":{"msg":"<script>&</script>"}}`) {
		t.Fatalf("payload was HTML-escaped by the encoder:\n%s", stdout)
	}
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	want := `{"status":"ok","version":"0.1.0"}`
	if got := drainBody(t, resp); got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
	if ts.Stdout.Len() != 0 {
		t.Fatalf("stdout not empty: %q", ts.Stdout.String())
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Post(ts.URL+"/health", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if got := drainBody(t, resp); got != `{"error":"method not allowed"}` {
		t.Fatalf("response body = %q", got)
	}
}

func TestStartupBanner(t *testing.T) {
	stdout := &syncBuffer{}
	stderr := &syncBuffer{}

	handler := server.NewHandler("mock", output.NewWriter(stdout), stderr, 2*1024*1024)
	httpSrv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Path:    "/mock",
		Version: "v0.1.0",
		Timeout: 10 * time.Second,
		Handler: handler,
		ErrOut:  stderr,
	})

	ln, err := httpSrv.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	want := fmt.Sprintf("webhookd v0.1.0 %s listening on %s, endpoint POST /mock\n",
		emDash, ln.Addr().String())
	if got := stderr.String(); got != want {
		t.Fatalf("banner =\n  %q\nwant:\n  %q", got, want)
	}
}
