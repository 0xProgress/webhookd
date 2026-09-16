# webhookd — Core Build Document

> The Unix pipe for webhooks. Receive, verify, and stream webhook events to stdout.

---

## What This Document Covers

This is the build document for the **core** of webhookd. The core is the
complete, working foundation that providers are built on top of. It ships
with one provider — `mock` — which exists as a reference implementation and
as the test double for the server pipeline. No real provider (`github`,
`stripe`, `slack`, `shopify`) is part of the core.

When the core ships, it is fully functional — it accepts webhooks, verifies
them, and streams verified events to stdout. A developer can implement a
provider against this core on day one.

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
| `payload` | object | yes | Full JSON body of the request |

`verified` is always `true` on stdout. A failed verification produces a 401
response and a stderr log line. Nothing goes to stdout.

**All seven fields are always present.** Fields without a value are emitted as
`""` (empty string), never omitted. The shape is identical for every event from
every provider — that is what makes `webhookd | jq` work without a
provider-specific filter.

`payload` is emitted from the raw request bytes. Key order, duplicate keys, and
numeric precision are preserved exactly as the provider sent them. The only
transformation applied is whitespace normalisation performed by the JSONL
encoder: the value is emitted compact on one line, with insignificant
whitespace removed and embedded newlines escaped. Structure and numbers are
not touched.

---

## The Provider Interface

This is the single most important thing in the codebase. Every provider
implements this contract exactly.

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

Every provider registers itself in `init()`:

```go
func init() {
	providers.Register(&StripeProvider{})
}
```

**All providers live in this repository**, under `providers/<name>/`. There is
no external provider mechanism: a new provider is a PR against this repo, not
a separate module that users import. The set of providers a binary supports is
fixed at build time.

A provider package's `init()` runs because its `cmd/<name>.go` file imports the
package. That command file is also where the provider's subcommand is wired
into the CLI and where any provider-specific defaults are declared — for
example, the conventional environment variable name for the signing secret.
Nothing else in the codebase needs to know the provider exists: the `init()`
call performs registration, and the registry is what the server consults at
request time.

Adding a provider is therefore three files in one PR:

```
providers/<name>/<name>.go        — the Provider implementation
providers/<name>/<name>_test.go   — the tests
cmd/<name>.go                     — the subcommand, which imports the above
```

---

## Repository Structure

```
webhookd/
│
├── main.go                        # Entry point — wires the root command
│
├── cmd/
│   ├── root.go                    # Root command, shared flags, provider dispatch
│   └── <name>.go                  # One file per provider subcommand
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
│   ├── event.go                   # Normalized Event struct
│   ├── writer.go                  # JSONL writer to stdout
│   └── pretty.go                  # Human-readable formatter
│
├── config/
│   └── config.go                  # Flag + env resolution, Config struct
│
├── docs/
│   ├── architecture.md            # How the core works
│   ├── webhookd-core.md           # This document — the build spec
│   ├── providers/
│   │   └── TEMPLATE.md            # Provider documentation template
│   └── contributing/
│       ├── provider-guide.md      # Full guide for writing a provider
│       └── checklist.md           # Provider checklist
│
├── scripts/
│   ├── AGENTS.md                  # Operating instructions for AI coding agents
│   └── setup-repo.sh
│
├── .github/
│   ├── CODEOWNERS
│   ├── dependabot.yml
│   ├── ISSUE_TEMPLATE/
│   ├── pull_request_template.md
│   ├── SECURITY.md
│   └── workflows/
│       ├── ci.yml
│       ├── codeql.yml
│       └── release.yml
│
├── .commitlintrc.json
├── .gitignore
├── .goreleaser.yaml
├── .golangci.yml
├── CHANGELOG.md
├── CONTRIBUTING.md
├── Dockerfile
├── LICENSE
├── Makefile
├── README.md
├── SECURITY.md
└── go.mod
```

`scripts/AGENTS.md` is intentionally not committed — `scripts/` is listed in
`.gitignore`. It is a local operating document, not a shipped artifact.

---

## Request Handling Order

This order is mandatory. Any deviation breaks signature verification.

```
1.  Reject if method is not POST (405)
2.  Reject if Content-Type is not application/json (415)
3.  Wrap r.Body in http.MaxBytesReader(w, r.Body, maxBody)
4.  Read entire raw body into []byte — a read error from step 3's wrapper
    is reported as 413, any other read error as 500
5.  Look up provider in registry by name (404 if not found)
6.  Call provider.Verify(request, rawBody)
7.  If error:
      → write error to stderr
      → respond 401
      → return — nothing goes to stdout
8.  Call provider.EventType(request, rawBody)
9.  Call provider.DeliveryID(request)
10. Call provider.EventID(request, rawBody)
11. Validate that rawBody is syntactically valid JSON (500 if not)
12. Build normalized Event struct
13. Write one JSONL line to stdout
14. Respond 200 {"ok": true}
```

Steps 4 and 11 use the **same bytes**. Never re-encode between them.

Step 3 uses `http.MaxBytesReader`, which returns a distinct error during the
read in step 4 when the limit is exceeded. That error is what drives the 413
response. The limit is therefore enforced *during* the read, not after it — an
oversized body is never fully buffered. This is the mechanism the security
requirement "body size limit enforced before read" refers to.

Verification (step 6) runs before JSON validity is checked (step 11) and before
`payload` is emitted. A request with a bad signature is rejected before any
parsing of its body, so a malformed or hostile body cannot reach the decoder.
Signature verification is a security boundary.

The provider lookup in step 5 is by name; the name is extracted from the URL
path after routing. An unknown path yields 404 before any body is read.

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
| Malformed JSON body | `500 Internal Server Error` | `{"error": "internal error"}` |

All error responses go to the HTTP client. Nothing goes to stdout. The error
description goes to stderr.

### Content-Type policy

The core accepts requests whose `Content-Type` header begins with
`application/json`. A missing or mismatched `Content-Type` yields 415 before
the body is read. Real providers may send a charset suffix
(`application/json; charset=utf-8`); the prefix match accommodates this.

The policy is set by the core, not by the provider interface. A provider whose
upstream sends a different content type (form-encoded, for example) cannot be
supported without a change to this document.

---

## Security Requirements

| Requirement | Rule |
|-------------|------|
| MAC comparison | `hmac.Equal()` only. Never `==`. Never string comparison. |
| Timestamp validation | Where provider supports it: reject if older than 300 seconds |
| Body size limit | Default 2MB. Configurable via `--max-body`. Enforced during read via `http.MaxBytesReader`. |
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

This means: read the secret value from the environment variable named
`STRIPE_WEBHOOK_SECRET`. The secret value itself never appears in a flag.

When `--secret-env` is empty (`""`, the default), the subcommand for a
provider may supply a conventional default variable name — for example, the
`github` subcommand defaults to `GITHUB_WEBHOOK_SECRET`. The default is
declared in the provider's `cmd/<name>.go`, not in the core. The core only
knows what it is told via `--secret-env`.

### Config resolution order

```
1. CLI flag
2. Environment variable
3. Built-in default
```

### `--list`

Prints the name of every registered provider, one per line, to stdout, in
the order returned by `providers.All()`. Exits 0.

```
github
mock
slack
stripe
```

### `--version`

Prints a single line to stdout and exits 0.

```
webhookd v0.1.0
```

The version string is injected at build time via
`-ldflags "-X main.version=..."`. When unset (a plain `go build`), the
value is `dev` and the line reads `webhookd dev`.

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

### Diagnostic formats

Two diagnostic lines have fixed formats. Everything else on stderr is
free-form and not a contract.

**Startup banner** — printed once at startup, to stderr:

```
webhookd v0.1.0 — listening on 127.0.0.1:8080, endpoint POST /mock
```

The version is the same string as `--version`. The endpoint is
`<host>:<port>` followed by `POST` and the resolved path.

**Failed verification** — printed once per rejected request, to stderr:

```
webhookd: github: signature mismatch — 203.0.113.4
```

The format is `webhookd: <provider>: <reason> — <remote-ip>`. The remote IP
is taken from `r.RemoteAddr` with the port stripped. `X-Forwarded-For` is not
consulted — it is attacker-controlled unless webhookd is behind a trusted
proxy, and webhookd makes no assumption that it is.

The `<reason>` is the provider's own error string, verbatim.

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
github
──────────────────────────────────────
✓ Signature verified

Event:        pull_request
Delivery ID:  8a3f1b...
Received:     2026-09-15T19:44:03Z

{
  "action": "opened"
}
```

The provider line is the lowercase identifier returned by `Name()`. The core
does not know how to title-case it — `github` is not `GitHub` by any general
rule, and hard-coding a display name per provider would violate the
core/provider split.

Pretty output goes to stdout so it can be redirected. Startup info still goes
to stderr.

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

The version value is the same string as `--version` prints (without the
`webhookd ` prefix and without the leading `v`). When unset at build time,
the value is `dev`.

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

The mock provider has no secret. `--secret-env` is ignored for `mock`.

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
FROM golang:1.27-alpine AS builder
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

## Dependencies

The core prefers the standard library. The only external dependency in the
core is the CLI framework, `github.com/spf13/cobra`, used in `cmd/`. This is
a deliberate choice: the CLI surface (subcommands, shared flags, `--help`,
shell completion) is the one place where hand-rolled code would be more
error-prone and more verbose than a small, widely used dependency.

Providers must not add dependencies. A provider is standard-library-only —
that is a rule, not a preference, because a provider runs on the request path
and every dependency in that path is a supply-chain risk.

The review criterion is: does the dependency earn its place on the request
path or in the CLI? If not, it does not belong.

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
- [ ] `--version` prints the version
- [ ] `--pretty` produces human-readable output
- [ ] Startup banner and failed-verification formats match this document
- [ ] Health endpoint returns 200
- [ ] `make check` passes clean
- [ ] Single binary builds for all five platforms
- [ ] README five-minute demo works end to end with mock provider
- [ ] CONTRIBUTING.md and provider guide are complete
- [ ] Provider documentation template exists

---

## What Ships After Core

Once the core is tagged and released, providers follow as separate PRs. Each
provider is independent. The order is:

1. `providers/github` — simplest verification scheme, good first provider
2. `providers/stripe` — timestamp validation adds complexity
3. `providers/slack` — base string construction is the tricky part
4. `providers/shopify` — base64-encoded HMAC, no timestamp

Community can pick up any of these or add new ones. The core does not need to
change for any of them.
