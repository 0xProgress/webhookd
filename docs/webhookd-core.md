# webhookd — Core Build Document

> The Unix pipe for webhooks. Receive, verify, and stream webhook events to stdout.

---

## What This Document Covers

This is the build document for the **core** of webhookd. No providers are included in the core. The core is the complete, working foundation that providers are built on top of.

When the core ships, it is fully functional — it just has no built-in providers yet. A developer can implement a provider against this core on day one.

---

## Core Responsibilities

The core does exactly these things and nothing else:

- Accept incoming HTTP POST requests
- Read the raw request body before anything else
- Hand the raw body and request to a registered provider
- Write one JSONL line to stdout per verified event
- Write all diagnostics to stderr
- Respond to the webhook provider with the correct HTTP status
- Expose a health endpoint
- Provide the CLI skeleton

The core does **not**:

- Know anything about Stripe, GitHub, Slack, or any other provider
- Make decisions about what an event means
- Store anything
- Retry anything
- Forward anything

---

## Normalized Output Shape

Every provider, regardless of implementation, produces this exact structure on stdout:

```json
{
  "provider": "stripe",
  "verified": true,
  "event": "payment_intent.succeeded",
  "id": "evt_123",
  "delivery_id": "optional-per-provider",
  "received_at": "2026-09-15T19:42:13Z",
  "payload": {}
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | yes | Lowercase provider name |
| `verified` | bool | yes | Always `true` on stdout — failures never reach output |
| `event` | string | yes | Provider-specific event type string |
| `id` | string | no | Event ID if the provider supplies one, else `""` |
| `delivery_id` | string | no | Delivery/request ID if the provider supplies one, else `""` |
| `received_at` | string | yes | ISO 8601 UTC timestamp of receipt |
| `payload` | object | yes | Full parsed JSON body |

`verified` is always `true` on stdout. A failed verification produces a 401 response and a stderr log line. Nothing goes to stdout.

---

## The Provider Interface

This is the single most important thing in the codebase. Every provider — built-in or community — implements this contract exactly.

```go
package providers

import "net/http"

// Provider is the interface every webhook provider must implement.
//
// A provider owns its signature verification scheme entirely.
// The core makes no assumptions about how any provider signs requests.
// Implementations must use constant-time comparison for all MAC operations.
type Provider interface {
	// Name returns the lowercase provider identifier.
	// This becomes the CLI subcommand and the "provider" field in output.
	// Examples: "stripe", "github", "slack", "shopify"
	Name() string

	// Verify checks the request signature against rawBody.
	// rawBody is the original request body bytes, unmodified.
	// Returns nil on success, a descriptive error on failure.
	// Errors are written to stderr; the request receives a 401.
	// Implementations MUST use hmac.Equal() for all MAC comparisons.
	Verify(r *http.Request, rawBody []byte) error

	// EventType extracts the event type string from the request.
	// This becomes the "event" field in output.
	// Return an empty string if the provider does not supply an event type.
	EventType(r *http.Request, rawBody []byte) string

	// DeliveryID returns a unique delivery or request ID.
	// This becomes the "delivery_id" field in output.
	// Return an empty string if the provider does not supply one.
	DeliveryID(r *http.Request) string

	// EventID returns the event's own ID.
	// This becomes the "id" field in output.
	// Return an empty string if the provider does not supply one.
	EventID(r *http.Request, rawBody []byte) string
}
```

### Provider registration

Providers self-register using `init()`. The registry is in `providers/registry.go`.

```go
package providers

var registry = map[string]Provider{}

// Register adds a provider to the global registry.
// Called from provider init() functions.
// Panics if a provider with the same name is already registered.
func Register(p Provider) {
	if _, exists := registry[p.Name()]; exists {
		panic("webhookd: provider already registered: " + p.Name())
	}
	registry[p.Name()] = p
}

// Get retrieves a registered provider by name.
func Get(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

// All returns all registered provider names.
func All() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
```

Built-in providers register in `init()`:

```go
func init() {
	providers.Register(&StripeProvider{})
}
```

Community providers do the same. Adding a community provider to a build is one blank import:

```go
import (
	_ "github.com/community/webhookd-shopify"
	_ "github.com/community/webhookd-discord"
)
```

---

## Repository Structure

```
webhookd/
│
├── main.go                        # Entry point — wires cobra root
│
├── cmd/
│   └── root.go                    # Root cobra command, shared flags, provider dispatch
│
├── server/
│   ├── server.go                  # HTTP listener, routing, timeouts
│   ├── handler.go                 # Request handler — body capture, provider call, output
│   └── server_test.go
│
├── providers/
│   ├── provider.go                # Provider interface definition
│   ├── registry.go                # Register / Get / All
│   ├── registry_test.go
│   └── mock/
│       ├── mock.go                # Reference implementation for contributors
│       └── mock_test.go
│
├── output/
│   ├── writer.go                  # JSONL writer to stdout
│   ├── pretty.go                  # Human-readable formatter
│   └── event.go                   # Normalized Event struct
│
├── config/
│   └── config.go                  # Flag + env resolution, Config struct
│
├── docs/
│   ├── architecture.md            # How the core works
│   ├── providers/
│   │   └── TEMPLATE.md            # Provider documentation template
│   └── contributing/
│       ├── provider-guide.md      # Full guide for writing a provider
│       └── checklist.md           # Provider checklist
│
├── .github/
│   └── workflows/                 # See repo-workflows document
│
├── CONTRIBUTING.md
├── CHANGELOG.md
├── LICENSE
├── Makefile
├── Dockerfile
├── .goreleaser.yaml
└── README.md
```

---

## Request Handling Order

This order is mandatory. Any deviation breaks signature verification.

```
1.  Enforce body size limit (default 2MB)
2.  Read entire raw body into []byte — store it, close nothing
3.  Look up provider in registry by name
4.  Call provider.Verify(request, rawBody)
5.  If error:
      → write error to stderr
      → respond 401
      → return — nothing goes to stdout
6.  Call provider.EventType(request, rawBody)
7.  Call provider.DeliveryID(request)
8.  Call provider.EventID(request, rawBody)
9.  JSON-decode payload from rawBody
10. Build normalized Event struct
11. Write one JSONL line to stdout
12. Respond 200 {"ok": true}
```

Step 2 and step 9 use the **same bytes**. Never re-encode between them.

---

## HTTP Response Behaviour

| Condition | Status | Body |
|-----------|--------|------|
| Valid, verified | `200 OK` | `{"ok": true}` |
| Invalid signature | `401 Unauthorized` | `{"error": "signature verification failed"}` |
| Provider not found | `404 Not Found` | `{"error": "unknown provider"}` |
| Wrong method | `405 Method Not Allowed` | `{"error": "method not allowed"}` |
| Body too large | `413 Payload Too Large` | `{"error": "request body too large"}` |
| Bad content type | `415 Unsupported Media Type` | `{"error": "unsupported content type"}` |
| Internal error | `500 Internal Server Error` | `{"error": "internal error"}` |

All error responses go to the HTTP client. Nothing goes to stdout. The error description goes to stderr.

---

## Security Requirements

| Requirement | Rule |
|-------------|------|
| MAC comparison | `hmac.Equal()` only. Never `==`. Never string comparison. |
| Timestamp validation | Where provider supports it: reject if older than 300 seconds |
| Body size limit | Default 2MB. Configurable via `--max-body`. Enforced before read. |
| Read timeout | 10 seconds default. Connections cannot hang. |
| Write timeout | 10 seconds default. |
| Bind address | Default `127.0.0.1`. Never `0.0.0.0` by default. |
| Public bind | Must be explicit: `--host 0.0.0.0` |

---

## CLI Interface

```
webhookd <provider> [flags]
webhookd --list
webhookd --version
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--secret-env` | `""` | Name of env var holding the signing secret |
| `--port` | `8080` | HTTP port |
| `--host` | `127.0.0.1` | Bind address |
| `--path` | `/<provider>` | Endpoint path |
| `--pretty` | off | Human-readable output |
| `--max-body` | `2097152` | Max body bytes (2MB) |
| `--timeout` | `10` | Read/write timeout in seconds |
| `--list` | — | List all registered providers and exit |
| `--version` | — | Print version and exit |

### Secret resolution

```
--secret-env STRIPE_WEBHOOK_SECRET
```

This means: read the secret value from the environment variable named `STRIPE_WEBHOOK_SECRET`. The secret value itself never appears in a flag.

### Config resolution order

```
1. CLI flag
2. Environment variable
3. Built-in default
```

---

## stdout vs stderr Rule

| Stream | Content |
|--------|---------|
| `stdout` | JSONL event lines only — one per verified webhook |
| `stderr` | Startup messages, errors, diagnostics, signature failures |

This is what makes piping work:

```bash
webhookd github | jq '.payload.repository.full_name'
```

Startup messages and errors on stderr never contaminate the data stream.

---

## Output Modes

### Default — JSONL

```bash
webhookd stripe
```

One JSON object per line. Machine-readable. Pipeable.

```
{"provider":"stripe","verified":true,"event":"payment_intent.succeeded","id":"evt_123","delivery_id":"","received_at":"2026-09-15T19:42:13Z","payload":{}}
```

### Pretty mode

```bash
webhookd github --pretty
```

```
GitHub
──────────────────────────────────────
✓ Signature verified

Event:        pull_request
Delivery ID:  8a3f1b...
Received:     2026-09-15T19:44:03Z

{
  "action": "opened"
}
```

Pretty output goes to stdout so it can be redirected. Startup info still goes to stderr.

---

## Health Endpoint

```
GET /health
```

Response:

```json
{
  "status": "ok",
  "version": "0.1.0"
}
```

Always returns 200. No dependency checks. No metrics. Nothing more.

---

## The Mock Provider

The mock provider ships with the core. It exists for two purposes:

1. Gives contributors a complete, working reference implementation to copy
2. Enables integration testing of the core server without a real provider

```go
// providers/mock/mock.go
// MockProvider is a reference implementation of the Provider interface.
// It accepts any request with the header X-Mock-Signature: valid
// and rejects all others.
// It is not intended for production use.
```

Every test that exercises the server pipeline uses the mock provider.

---

## Makefile

```makefile
.PHONY: build test lint fmt vet clean release

build:
	go build -ldflags="-s -w -X main.version=$(shell git describe --tags --always)" -o bin/webhookd .

test:
	go test -race -count=1 ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

vet:
	go vet ./...

check: fmt vet lint test

clean:
	rm -rf bin/ coverage.out coverage.html

release:
	goreleaser release --clean
```

---

## Dockerfile

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o webhookd .

FROM scratch
COPY --from=builder /app/webhookd /webhookd
EXPOSE 8080
ENTRYPOINT ["/webhookd"]
```

---

## .goreleaser.yaml

```yaml
version: 2

builds:
  - env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - format: tar.gz
    format_overrides:
      - goos: windows
        format: zip

dockers:
  - image_templates:
      - ghcr.io/0xprogress/webhookd:{{.Version}}
      - ghcr.io/0xprogress/webhookd:latest

checksum:
  name_template: checksums.txt

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"
```

---

## v0.1 Acceptance Criteria

The core is shippable when:

- [ ] `Provider` interface is defined and stable
- [ ] `Register` / `Get` / `All` work correctly
- [ ] Duplicate registration panics with a clear message
- [ ] Mock provider passes all server pipeline tests
- [ ] Raw body is captured before any other processing
- [ ] Verified events produce correct JSONL on stdout
- [ ] Failed verification produces 401 and stderr log, nothing on stdout
- [ ] All HTTP status codes are correct per the table above
- [ ] Body size limit is enforced
- [ ] Read and write timeouts are set
- [ ] Default bind is `127.0.0.1`
- [ ] `--list` prints registered providers
- [ ] `--pretty` produces human-readable output
- [ ] Health endpoint returns 200
- [ ] `make check` passes clean
- [ ] Single binary builds for all five platforms
- [ ] README five-minute demo works end to end with mock provider
- [ ] CONTRIBUTING.md and provider guide are complete
- [ ] Provider documentation template exists

---

## What Ships After Core

Once the core is tagged and released, providers follow as separate PRs. Each provider is independent. The order is:

1. `providers/github` — simplest verification scheme, good first provider
2. `providers/stripe` — timestamp validation adds complexity
3. `providers/slack` — base string construction is the tricky part

Community can pick up any of these or add new ones. The core does not need to change for any of them.
