// Package server implements the HTTP listener for webhookd.
//
// The listener mounts a webhook handler at a configured path and
// exposes a health endpoint at /health. Request handling itself lives
// in handler.go; this file owns binding, routing, and shutdown.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Options configures a Server.
//
// Every field is set by the caller. Config resolution — flag,
// environment, default — belongs to the config package; server trusts
// what it is given and does not re-derive defaults.
type Options struct {
	// Host is the bind address. The default of 127.0.0.1 and the rule
	// that 0.0.0.0 must be explicit are enforced by the config layer,
	// not here. An empty Host binds to all interfaces, which is a
	// caller bug.
	Host string

	// Port is the TCP port. Port 0 asks the operating system to choose
	// a free port; tests use this to avoid collisions.
	Port int

	// Path is the endpoint the webhook Handler is mounted at. It must
	// begin with "/" and is matched exactly by the mux. Example:
	// "/mock".
	Path string

	// Version is the version string. It appears verbatim in the
	// startup banner and, with any leading "v" stripped, in the
	// /health response.
	Version string

	// Timeout is applied as both ReadTimeout and WriteTimeout on the
	// underlying http.Server.
	Timeout time.Duration

	// Handler is the webhook request handler. In production this is a
	// *Handler constructed by NewHandler.
	Handler http.Handler

	// ErrOut receives the startup banner. It is never used for event
	// data. Production passes os.Stderr. If nil, the banner is
	// suppressed.
	ErrOut io.Writer
}

// Server is the HTTP listener.
type Server struct {
	httpServer *http.Server
	version    string
	path       string
	errOut     io.Writer
}

// New builds a Server from opts.
//
// New does not bind a port; call ListenAndServe, or Listen followed by
// Serve. Separating construction from binding lets a test read the
// port the operating system actually assigned before issuing
// requests.
func New(opts Options) *Server {
	mux := http.NewServeMux()
	mux.Handle(opts.Path, opts.Handler)
	mux.Handle("/health", newHealthHandler(normalizeVersion(opts.Version)))

	return &Server{
		httpServer: &http.Server{
			Addr:         net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)),
			Handler:      mux,
			ReadTimeout:  opts.Timeout,
			WriteTimeout: opts.Timeout,
		},
		version: opts.Version,
		path:    opts.Path,
		errOut:  opts.ErrOut,
	}
}

// Listen binds the configured address and returns the listener. The
// caller is responsible for calling Serve on the returned listener.
//
// The startup banner is printed to ErrOut once the listener is open,
// so the address it reports is the address actually bound. That may
// differ from the configured port when the caller asked for port 0.
func (s *Server) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return nil, err
	}
	s.printBanner(ln.Addr().String())
	return ln, nil
}

// Serve serves requests on ln until the server is shut down.
//
// It always returns a non-nil error; http.ErrServerClosed indicates a
// clean shutdown.
func (s *Server) Serve(ln net.Listener) error {
	return s.httpServer.Serve(ln)
}

// ListenAndServe binds the configured address and serves until shut
// down. It is a convenience for the production path; tests use Listen
// and Serve separately so they can read the ephemeral port.
func (s *Server) ListenAndServe() error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Shutdown stops the server gracefully, allowing in-flight requests to
// complete or the context to expire.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// printBanner writes the startup banner to ErrOut.
//
// The format is fixed by docs/webhookd-core.md §"Diagnostic formats":
//
//	webhookd <version> — listening on <host>:<port>, endpoint POST <path>
//
// A nil ErrOut suppresses the banner, which is what the test suite
// wants so its captured stderr contains only the diagnostics under
// test.
func (s *Server) printBanner(addr string) {
	if s.errOut == nil {
		return
	}
	fmt.Fprintf(s.errOut, "webhookd %s — listening on %s, endpoint POST %s\n",
		s.version, addr, s.path)
}

// healthResponse is the JSON shape returned by GET /health. Field
// order matches the example in docs/webhookd-core.md §"Health
// Endpoint"; encoding/json emits struct fields in declaration order.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// newHealthHandler returns the /health handler.
//
// The response body is marshaled once at construction time. The
// handler writes it on every GET without touching any dependency, so
// the endpoint cannot fail — which is what "always returns 200" means
// in the spec.
//
// A non-GET method yields 405 with the standard error body. The spec
// states the endpoint as GET /health; other methods are not a health
// check and are rejected by the same rule that governs every other
// route.
func newHealthHandler(version string) http.Handler {
	body, err := json.Marshal(healthResponse{Status: "ok", Version: version})
	if err != nil {
		// Unreachable for this struct: its fields are strings and the
		// tags are fixed. Falling back to a static body keeps the
		// handler from panicking on a request path if this ever
		// changes.
		body = []byte(`{"status":"error"}`)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})
}

// normalizeVersion strips a leading "v" so that the banner can report
// "v0.1.0" while /health reports "0.1.0". The rule is stated in
// docs/webhookd-core.md §"Health Endpoint": the health version is the
// --version string without the "webhookd " prefix and without the
// leading "v".
func normalizeVersion(v string) string {
	if len(v) > 0 && v[0] == 'v' {
		return v[1:]
	}
	return v
}
