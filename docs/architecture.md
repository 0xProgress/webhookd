# Architecture

> How webhookd is put together, and why.

This document describes the core of webhookd. It is written for someone who is
about to read the code, add a provider, or review a change to the core. It is
not a user manual — see [README.md](../README.md) for that.

---

## What the core is

webhookd is a single binary that listens for webhook HTTP requests, verifies
their signatures, and writes one JSONL line per verified event to stdout.

The core is the part that does not know anything about any specific provider.
It owns:

- the HTTP listener and its timeouts
- raw body capture
- the provider registry
- the request lifecycle
- the output contract
- the CLI skeleton

It does not own signature schemes, event semantics, storage, retries, or
forwarding. Those belong to providers, or to nothing at all.

---

## Why a core/provider split

A webhook is an HTTP POST with a signature header. Every provider signs
differently — some sign the raw body, some sign a composed string with a
timestamp, some use base64, some hex. But the surrounding pipeline is identical
in every case: read the body, verify, extract a few fields, emit a line.

If that pipeline is duplicated per provider, every provider reimplements body
capture, error handling, and output formatting, and every provider gets those
subtly wrong in a different way. If the pipeline is centralized and providers
are leaf nodes, the security-critical parts are written once and reviewed once.

The core is the pipeline. A provider is a leaf node that answers five questions
about a request. That is the entire design.

---

## The pipeline

Every request follows this order. The order is mandatory — deviating from it
breaks signature verification.

```
1.  Enforce body size limit (default 2MB)
2.  Read entire raw body into []byte
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

Steps 2 and 9 use the same bytes. The raw body is captured once and never
re-encoded. If a provider hashes the body, it hashes the same bytes the caller
will eventually see in `payload`. This is the single most important invariant
in the codebase.

Verification happens before decoding. A request with a bad signature is
rejected before any JSON parsing, so a malformed or hostile body cannot reach
the decoder. This matters: signature verification is a security boundary, and
nothing crosses it until it passes.

---

## The provider contract

```go
type Provider interface {
    Name() string
    Verify(r *http.Request, rawBody []byte) error
    EventType(r *http.Request, rawBody []byte) string
    DeliveryID(r *http.Request) string
    EventID(r *http.Request, rawBody []byte) string
}
```

Five methods. Every provider implements exactly these. The interface is frozen
at v0.1 — if a provider needs something the interface does not provide, the
provider is wrong, not the interface.

`Name()` returns the lowercase identifier that becomes both the CLI subcommand
and the `provider` field in output. It is the key the registry uses. Two
providers cannot share a name.

`Verify()` is the security boundary. It receives the request and the raw body
and returns nil or an error. It is responsible for everything: reading the
signature header, reading any timestamp header, rejecting old timestamps,
computing the expected MAC, and comparing in constant time. The core makes no
assumptions about how this works. The only rule the core enforces is that
`Verify()` must use `hmac.Equal()` for any MAC comparison — this is checked in
review, not at runtime, because the core cannot see inside the provider's
implementation.

`EventType()` extracts the event type string. It may read a header, parse the
body, or both. It returns an empty string if the provider does not supply one.

`DeliveryID()` returns a delivery or request identifier. It only receives the
request, not the body, because delivery IDs are always in headers for the
providers that have them. Returns an empty string if absent.

`EventID()` returns the event's own ID. It receives the body because event IDs
are often in the JSON payload. Returns an empty string if absent.

The split between `DeliveryID` and `EventID` reflects a real distinction:
a delivery ID identifies _this attempt to deliver an event_, and an event ID
identifies _the event itself_. Stripe, for example, has an event ID on the
event object and no separate delivery ID. GitHub has a `X-GitHub-Delivery`
header and no event ID in the body. Both fields are optional and default to
the empty string.

---

## The registry

Providers self-register in `init()`:

```go
func init() {
    providers.Register(&StripeProvider{})
}
```

The registry is a `map[string]Provider` guarded by a duplicate check that
panics. The panic is intentional: a duplicate registration is a programming
error that should be caught at startup, not a runtime condition to handle.
Registering the same name twice means two packages think they own the same
subcommand, and there is no correct way to resolve that at runtime.

Registration happens through `init()` rather than an explicit registration
list in `main.go` so that adding a community provider is one blank import:

```go
import (
    _ "github.com/0xProgress/webhookd/providers/shopify"
)
```

The blank import runs the provider's `init()`, which calls `Register`. Nothing
else is required. The core does not need to know the provider exists until a
request arrives for it.

`Get` and `All` are the read side. `Get` is called once per request, after the
provider name is extracted from the URL path. `All` is called once at startup
for `--list`.

---

## The output contract

Every verified event produces exactly one JSON object on stdout, one per line,
with this shape:

```json
{
  "provider": "stripe",
  "verified": true,
  "event": "payment_intent.succeeded",
  "id": "evt_123",
  "delivery_id": "",
  "received_at": "2026-09-15T19:42:13Z",
  "payload": {}
}
```

The shape is identical across every provider. A consumer that parses one
webhookd event can parse all of them, regardless of which provider produced
it. This is what makes `webhookd | jq` work without a provider-specific filter.

`verified` is always `true` in output. Failed verification never reaches
stdout. The field exists so that consumers can rely on a stable schema and so
that the semantic meaning of the line is self-describing — the presence of the
line is the assertion that verification succeeded.

`received_at` is the time the core received the request, not the time the
provider generated the event. Provider timestamps, when present, live inside
`payload`. This distinction matters when the two are far apart, which is
usually a sign of replay or clock skew.

`payload` is the full parsed JSON body, unmodified. webhookd does not strip,
reshape, or interpret it. If you need the raw bytes exactly as the provider
sent them, they are in `payload` as a decoded object — the original bytes are
not preserved on stdout because JSONL is line-oriented and raw bytes can
contain newlines.

---

## stdout vs stderr

| Stream | Content                                                 |
| ------ | ------------------------------------------------------- |
| stdout | JSONL event lines, one per verified webhook             |
| stderr | Startup banner, errors, diagnostics, signature failures |

This is the contract that makes piping work. A user should be able to write:

```bash
webhookd github | jq '.payload.repository.full_name'
```

and never see a startup message, a warning, or a signature failure in the
middle of their JSON stream. Any write to stdout that is not a complete JSON
line is a bug.

Errors go to stderr because they are diagnostics, not data. A failed
verification is interesting to the operator watching the terminal, but it is
not an event the consumer of the stream should have to filter out.

Pretty mode is the one exception to "stdout is JSONL": with `--pretty`, stdout
carries the human-readable form instead. This is a deliberate trade-off — pretty
mode exists for live demos and manual inspection, where piping is not the goal.
The startup banner still goes to stderr in both modes.

---

## Security model

The core's security posture is that a webhookd instance is safe by default on
a developer machine and explicit when exposed.

| Concern             | Mitigation                                                               |
| ------------------- | ------------------------------------------------------------------------ |
| Signature forgery   | Provider `Verify()` uses `hmac.Equal()` for constant-time comparison     |
| Replay              | Providers that support timestamps reject requests older than 300 seconds |
| Body flooding       | Body size limit enforced before read, default 2MB                        |
| Slowloris           | Read and write timeouts of 10 seconds                                    |
| Accidental exposure | Default bind is `127.0.0.1`; `0.0.0.0` requires `--host`                 |
| Secret leakage      | Secrets are read from environment variables, never from flags            |
| Log injection       | Payload content is never interpolated into log lines unsanitized         |

The core cannot verify that a provider's `Verify()` is correct. It can only
verify that the provider called `Register`, that it returned nil or an error,
and that on error the request was rejected. Everything about the correctness of
a provider's signature scheme is the provider's responsibility and is
reviewed against the provider checklist in `CONTRIBUTING.md`.

Providers must not log the secret, the signature, or the body. The core does
not do this, but a provider that does would be leaking material to stderr that
could be captured by log aggregation. This is a review item, not a runtime
check.

---

## What the core deliberately does not do

Understanding what is missing is as important as understanding what is there.

**No storage.** Events are not persisted. If nothing is reading stdout, events
are lost. This is intentional: webhookd is a stream, not a queue. Durability
is the caller's job — pipe to `tee`, `jq`, a file, or a queue.

**No retries.** If the consumer of stdout is slow or blocked, the HTTP
response is still sent. webhookd does not buffer events waiting for a
consumer. The provider decides whether to retry based on the HTTP response
status, which is why a verified event returns 200 immediately.

**No forwarding.** webhookd does not send events anywhere. It writes to
stdout. If you want to forward, pipe to something that forwards.

**No interpretation.** The core does not know what `payment_intent.succeeded`
means or what a `pull_request.opened` payload contains. It extracts the event
type, the IDs, and the payload, and emits them. Meaning is the consumer's
problem.

**No persistence of state between requests.** Every request is independent.
There is no session, no cursor, no offset. Restarting webhookd loses nothing
because there was nothing to lose.

**No metrics, no tracing, no dashboards.** The health endpoint returns 200 and
a version. That is the entire observability surface.

Each of these omissions is a deliberate scope decision. Adding any of them
would make webhookd a different tool.

---

## Extension points

There are exactly two ways to extend webhookd:

1. **Add a provider.** Implement the `Provider` interface, call `Register` in
   `init()`, add a subcommand file in `cmd/`. See [CONTRIBUTING.md](../CONTRIBUTING.md).

2. **Pipe the output somewhere.** The JSONL contract is stable. Anything that
   reads newline-delimited JSON can consume webhookd's output.

There is no plugin system, no configuration file, no dynamic loading. A
provider is a Go package. Adding it means rebuilding the binary. This is
deliberate: a static binary with a fixed set of providers is easier to audit
than a dynamic loader, and the build is fast enough that this is not a real
constraint.

---

## What ships after core

The core is complete and shippable before any real provider exists. The mock
provider exercises the full pipeline and is the reference implementation for
contributors. Real providers — `github`, `stripe`, `slack` — are separate PRs
against a frozen core.

This ordering exists so that the core's interface can be reviewed, tested, and
released without the pressure of a specific provider shaping it. If the first
provider written were Stripe, the interface might grow a timestamp field
because Stripe needs one. By shipping the core first, with only the mock as a
consumer, the interface stays minimal and general.
