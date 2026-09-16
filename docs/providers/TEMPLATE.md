# Writing a Provider

> The long-form guide for adding a webhook provider to webhookd.

This is the deep version. The short version is in
[CONTRIBUTING.md §"Adding a Provider"](../../CONTRIBUTING.md#adding-a-provider).
Read that first if you have not. This document explains *why* the short
version says what it says.

---

## What you are building

A provider is a leaf node. It answers five questions about one HTTP request
and returns. It does not read the environment, does not talk to the network,
does not write to stdout or stderr, and does not know whether other providers
exist. The core is the pipeline; you are one stage of it.

That smallness is deliberate. Everything that runs before your code — the
HTTP listener, the body capture, the size limit, the routing — is written
once and reviewed once. Everything that runs after your code — the JSONL
encoding, the stream routing, the response — is written once and reviewed
once. The narrow part of the funnel is the part you own.

---

## The five methods

`providers/provider.go` defines the interface. Read the doc comments there;
they are the contract. What follows is the "why" behind each one.

### `Name() string`

The lowercase identifier. It becomes the CLI subcommand, the `provider`
field in output, and the registry key. Two providers cannot share a name;
`Register` panics on a duplicate.

Do not title-case it. The core does not know how to render `github` as
`GitHub` and would rather show the lowercase form than maintain a
display-name table. Everything downstream, including pretty mode, prints the
name verbatim.

### `Verify(r *http.Request, rawBody []byte) error`

The security boundary. Nothing crosses it until it returns nil.

`rawBody` is the exact bytes the client sent. If the provider signs the raw
body, hash `rawBody` directly. If it signs a composed string —
`{timestamp}.{rawBody}`, or `v0:{timestamp}:{rawBody}`, or anything else —
build that string and hash *that*. Do not re-read `r.Body`; it has already
been consumed and will return zero bytes. Do not call `json.Unmarshal` on
`rawBody` before hashing it; the JSON decoder does not preserve the original
byte sequence, and any hash computed after decoding will not match what the
provider sent.

Return an error on any failure. The error string is written to stderr and
the client receives a 401. Nothing on the error path reaches stdout.

The one hard rule: **use `hmac.Equal` for every MAC comparison.** See
[Why `hmac.Equal`](#why-hmacequal) below.

### `EventType(r *http.Request, rawBody []byte) string`

The event type. Some providers put it in a header (`X-GitHub-Event`), some in
the body (`"type": "payment_intent.succeeded"`), some in both. Read whichever
the provider's documentation specifies.

Return `""` if the provider does not supply one. Do not invent a placeholder
and do not return an error — the method signature has no error return, and
the pipeline treats `""` as "no event type" rather than a failure.

If the field lives in the body, decode only what you need. A tiny anonymous
struct with one field is enough:

```go
var envelope struct {
    Type string `json:"type"`
}
if err := json.Unmarshal(rawBody, &envelope); err != nil {
    return ""
}
return envelope.Type
```

Decoding the whole body into `map[string]any` is a mistake — it costs more
than it saves, and the fields you never read can mislead the next reader
into thinking they are part of the contract.

### `DeliveryID(r *http.Request) string`

The delivery or request identifier, if the provider supplies one. This method
receives only the request, not the body, because delivery IDs are always in
headers for the providers that have them.

Return `""` if absent. Return `""` even if the body contains something that
looks like an ID — that is what `EventID` is for. Do not pull a header that
is not documented as a delivery ID; the field is optional and an empty string
is honest, while a guess is not.

### `EventID(r *http.Request, rawBody []byte) string`

The event's own ID, if the provider supplies one. This method receives the
body because event IDs are often in the payload. Return `""` if absent.

The distinction from `DeliveryID` is real and worth understanding. A delivery
ID identifies *this attempt to deliver an event* — a retry has a new delivery
ID but the same event ID. An event ID identifies *the event itself*. Stripe
has an event ID on the event object and no separate delivery ID. GitHub has
`X-GitHub-Delivery` and no event ID in the body. Both fields are optional.

---

## Why `hmac.Equal`

A MAC comparison with `==` leaks information through timing. Every byte that
matches lets the comparison run a few nanoseconds longer before it fails, and
an attacker who can measure response time can recover the correct signature
one byte at a time. This is a well-understood attack, and it works against
naive code.

`hmac.Equal` is constant-time: it takes the same amount of time whether the
two inputs differ in the first byte or the last. That is the only reason it
exists and the only reason the rule is absolute.

Never use:

```go
if sig == expected { ... }                   // leaks
if strings.Compare(sig, expected) == 0 { }   // leaks
if bytes.Equal(sigBytes, expectedBytes) { }  // leaks
```

Always use:

```go
if hmac.Equal([]byte(sig), []byte(expected)) { ... }  // constant-time
```

The comparison leaks only if the attacker can measure the timing of a request
they control. That is why the rule applies to MAC comparisons and not to,
say, comparing a public provider name against a registry key.

---

## Timestamp validation

Providers that include a timestamp in the signature — Stripe, Slack, and
others — are protecting against replay. Without a timestamp check, an
attacker who captures one valid request can send it again tomorrow, and the
signature will still verify because the body has not changed.

The timestamp is part of the signed string, so an attacker cannot alter it
without invalidating the signature. The check is: parse the timestamp,
compare it against the current time, and reject if the difference exceeds
the tolerance. The spec fixes the tolerance at 300 seconds.

**Order matters.** Validate the timestamp *after* computing and comparing the
MAC, not before. If you reject an old timestamp before checking the signature,
an attacker can distinguish "your timestamp is wrong" from "your signature is
wrong" by watching the error response — and can use that to enumerate valid
requests. Both checks should produce the same error string.

The correct order:

1. Read the signature header.
2. Read the timestamp header.
3. Parse the signature into `(scheme, hex_digest)`. Reject if malformed.
4. Compute `HMAC-SHA256(secret, signedString)` where `signedString` includes
   the timestamp.
5. Compare with `hmac.Equal`. Reject if it fails.
6. Parse the timestamp.
7. Reject if the timestamp is more than 300 seconds old.

Step 7 runs only if steps 1–6 succeed. Every rejection produces the same
error string on stderr and the same 401 to the client.

A named constant for the tolerance keeps the 300 out of the middle of the
function:

```go
const maxTimestampAge = 300 * time.Second
```

---

## A worked example: GitHub

GitHub is the simplest real provider. Reading its implementation is a good
way to see the shape. The code below is illustrative; it is not in this
repository yet.

GitHub's signature scheme, from
[Validating webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries):

| Detail | Value |
|---|---|
| Signature header | `X-Hub-Signature-256` |
| Signature format | `sha256=<hex_digest>` |
| Algorithm | HMAC-SHA256 |
| What is signed | The raw request body |
| Timestamp header | None |
| Event type header | `X-GitHub-Event` |
| Delivery ID header | `X-GitHub-Delivery` |

The implementation:

```go
package github

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "net/http"
    "os"
    "strings"

    "github.com/0xProgress/webhookd/providers"
)

func init() {
    providers.Register(&Provider{})
}

type Provider struct{}

func (p *Provider) Name() string { return "github" }

func (p *Provider) Verify(r *http.Request, rawBody []byte) error {
    secret := []byte(os.Getenv("GITHUB_WEBHOOK_SECRET"))
    if len(secret) == 0 {
        return errors.New("github: GITHUB_WEBHOOK_SECRET is not set")
    }

    sig := r.Header.Get("X-Hub-Signature-256")
    if sig == "" {
        return errors.New("github: missing X-Hub-Signature-256 header")
    }

    // GitHub's format is "sha256=<hex>". Strip the prefix.
    const prefix = "sha256="
    if !strings.HasPrefix(sig, prefix) {
        return errors.New("github: signature missing sha256= prefix")
    }
    got := sig[len(prefix):]

    mac := hmac.New(sha256.New, secret)
    mac.Write(rawBody)
    want := hex.EncodeToString(mac.Sum(nil))

    if !hmac.Equal([]byte(got), []byte(want)) {
        return errors.New("github: signature mismatch")
    }
    return nil
}

func (p *Provider) EventType(r *http.Request, rawBody []byte) string {
    return r.Header.Get("X-GitHub-Event")
}

func (p *Provider) DeliveryID(r *http.Request) string {
    return r.Header.Get("X-GitHub-Delivery")
}

func (p *Provider) EventID(r *http.Request, rawBody []byte) string {
    return ""
}
```

Several things to notice:

- `Verify` reads the secret from the environment on every call. That is
  cheap and avoids caching state across requests.
- The signature comparison is `hmac.Equal` on the hex-decoded halves, not
  on the full `sha256=…` header. Stripping the prefix first is cleaner than
  comparing the whole string, but both work as long as the comparison is
  constant-time.
- `EventID` returns `""`. GitHub does not have a top-level event ID. Guessing
  a value from `payload.pull_request.id` would be wrong — that is the pull
  request's ID, not the event's.
- The error strings are all prefixed `github:`. That prefix is what appears
  in the stderr diagnostic.

Adding this provider to the repository would be three files:
`providers/github/github.go`, `providers/github/github_test.go`, and
`cmd/github.go`.

---

## The three files

A provider PR adds exactly three files. Nothing else.

```
providers/<name>/<name>.go        — the Provider implementation
providers/<name>/<name>_test.go   — the tests
cmd/<name>.go                     — the subcommand, which imports the above
```

No `go.mod` change. No new dependency. No edit to any existing file. If your
provider seems to need a change to the core, that is a signal that either the
provider is wrong (it is trying to do something that belongs in the core) or
the interface is wrong (it needs something the interface does not provide,
which is a spec change and needs discussion first).

---

## `cmd/<name>.go`

The subcommand file does three things:

1. **Imports the provider package**, so its `init()` runs and it registers
   itself. This is the only reason the provider appears at all — without
   the import, the provider is compiled out and the binary does not know it
   exists.
2. **Declares the subcommand** and adds it to the root command during
   `init()`.
3. **Sets any provider-specific defaults**, most commonly the conventional
   environment variable name for the signing secret.

The pattern is the same for every provider. Look at `cmd/mock.go` for the
simplest working example. When your provider's secret has a well-known name —
`GITHUB_WEBHOOK_SECRET`, `STRIPE_WEBHOOK_SECRET`, `SLACK_SIGNING_SECRET` — set
it as the default in your subcommand's flag declaration. Users who have the
variable set can then omit `--secret-env` entirely.

A provider whose secret name is not conventional should not set a default;
the user must pass `--secret-env` explicitly.

---

## Common mistakes

**Decoding the body before hashing it.** The most common error. Hash the
`rawBody` slice you received. If you find yourself reaching for `r.Body`,
you have already lost — it was consumed by the core before `Verify` was
called.

**Using `==` for the signature comparison.** `hmac.Equal` is not a
stylistic preference. See [Why `hmac.Equal`](#why-hmacequal).

**Rejecting old timestamps before checking the signature.** This leaks
information about which signatures are valid. See
[Timestamp validation](#timestamp-validation) for the correct order.

**Returning a value from `EventID` that is not the event's ID.** A pull
request's ID, a message's ID, a delivery's ID — none of these are the event's
ID. If the provider does not supply one, return `""`.

**Assuming `r.Body` is re-readable.** It is not. `http.Request.Body` is a
one-shot stream. The core reads it into `rawBody` before calling you.

**Reading the environment in `init()` or in the `Provider` struct's
constructor.** Secrets can change between process start and a request, and
`Verify` is where you need the current value. Read it there.

**Logging.** Do not log the secret, the signature, or the body. The core
does not, and a provider that does would be leaking material to stderr.

**Adding a dependency.** Providers are standard-library only. If you find
yourself reaching for a third-party HTTP client or a JSON library, stop and
ask — the core exists to prevent this.

---

## Where to look when stuck

1. **`providers/provider.go`** — the interface's doc comments. The contract
   you are implementing.
2. **`providers/mock/mock.go`** — a complete, minimal implementation. Your
   provider is the same shape with a real signature scheme.
3. **`docs/webhookd-core.md`** — the build spec. Every rule the core enforces
   about providers is stated there.
4. **`docs/architecture.md`** — the reasoning. Why the interface has five
   methods, why `Verify` runs before decoding, why the output shape is fixed.
5. **`docs/providers/TEMPLATE.md`** — the documentation you will write.
6. **Your provider's own webhook documentation.** The only authoritative
   source for the signature scheme, the header names, and the payload shape.

If after all six the answer is still unclear, open an issue describing the
ambiguity. Do not invent behaviour. The interface is frozen at v0.1 and any
change needs discussion before code.