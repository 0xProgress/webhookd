# webhookd

> The Unix pipe for webhooks. Receive, verify, and stream webhook events to stdout.

`webhookd` is a single binary that listens for webhooks, verifies their signatures,
and prints one JSON line per verified event to stdout. Pipe it anywhere.

```bash
webhookd github | jq '.payload.pull_request.title'
webhookd stripe | grep payment_intent.succeeded | jq '.payload.amount'
webhookd shopify | tee -a orders.jsonl
```

No database. No dashboard. No queue. No retries. No interpretation of what an
event means. Just verified events, on a stream, where Unix tools can reach them.

---

## Why

Webhook debugging usually means one of three things:

- A hosted service that stores your payloads and shows them in a web UI.
- A hand-rolled HTTP server you rewrite every time you add a provider.
- `ngrok` plus `nc` plus hope.

All three are heavier than the problem. A webhook is an HTTP POST with a signature
header. Verify the signature, print the body, exit the process when you're done.
That's a Unix filter. It should compose like one.

`webhookd` is that filter. It knows how to verify signatures for a set of providers,
it writes verified events as JSONL, and it gets out of the way.

---

## Install

### Homebrew (macOS, Linux)

```bash
brew install 0xProgress/tap/webhookd
```

### Go

```bash
go install github.com/0xProgress/webhookd@latest
```

Requires Go 1.25 or later.

### Docker

```bash
docker run --rm -p 8080:8080 \
  -e GITHUB_WEBHOOK_SECRET=your_secret \
  ghcr.io/0xprogress/webhookd:latest github --host 0.0.0.0
```

### Binary

Download the archive for your platform from the
[releases page](https://github.com/0xProgress/webhookd/releases), extract, and put
`webhookd` on your `PATH`.

---

## Five-minute demo

No provider, no secret, no network. The built-in `mock` provider accepts any request
with the header `X-Mock-Signature: valid` and rejects everything else. It exists so
you can see the pipeline end to end immediately.

**Terminal 1 — start the server:**

```bash
webhookd mock
```

You'll see a startup line on stderr:

```
webhookd v0.1.0 — listening on 127.0.0.1:8080, endpoint POST /mock
```

**Terminal 2 — send a webhook:**

```bash
curl -s -XPOST http://127.0.0.1:8080/mock \
  -H 'Content-Type: application/json' \
  -H 'X-Mock-Signature: valid' \
  -d '{
    "type": "order.created",
    "id": "evt_abc123",
    "data": {"order_id": "ord_999", "amount": 4200}
  }'
```

**Terminal 1 — one JSON line appears on stdout:**

```json
{"provider":"mock","verified":true,"event":"order.created","id":"evt_abc123","delivery_id":"","received_at":"2026-09-15T19:42:13Z","payload":{"type":"order.created","id":"evt_abc123","data":{"order_id":"ord_999","amount":4200}}}
```

**Now pipe it:**

```bash
webhookd mock | jq -c '{event, amount: .payload.data.amount}'
```

```
{"event":"order.created","amount":4200}
```

That's the whole tool. Everything below is providers and flags.

---

## Providers

All providers ship in this repository, under `providers/<name>/`. There is no
external provider mechanism — a new provider is a PR, not a separate module.
The set of providers a given binary supports is fixed at build time.

| Provider | Status | Signature | Timestamp check | Notes |
|---|---|---|---|---|
| `mock` | ✅ built-in | `X-Mock-Signature: valid` | No | Reference implementation. Not for production. |
| `github` | 🚧 planned | `X-Hub-Signature-256` HMAC-SHA256 | No | Simplest real provider. Good first contribution. |
| `stripe` | 🚧 planned | `Stripe-Signature` HMAC-SHA256 | Yes, 300s | Signs `{timestamp}.{body}`. |
| `slack` | 🚧 planned | `X-Slack-Signature` HMAC-SHA256 | Yes, 300s | Signs `v0:{timestamp}:{body}`. |
| `shopify` | 💡 wanted | `X-Shopify-Hmac-Sha256` base64 | No | [Open an issue](https://github.com/0xProgress/webhookd/issues/new/choose) if you want it. |

Want a provider that isn't listed? Open a
[provider request](https://github.com/0xProgress/webhookd/issues/new?template=provider_request.yml).
Implementing one is a well-scoped first contribution — see
[CONTRIBUTING.md](CONTRIBUTING.md).

---

## Usage

```
webhookd <provider> [flags]
webhookd --list
webhookd --version
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--secret-env` | `""` | Name of the environment variable holding the signing secret |
| `--port` | `8080` | HTTP port |
| `--host` | `127.0.0.1` | Bind address. Use `0.0.0.0` only behind a proxy you control. |
| `--path` | `/<provider>` | Endpoint path |
| `--pretty` | off | Human-readable output instead of JSONL |
| `--max-body` | `2097152` | Max request body in bytes (2MB) |
| `--timeout` | `10` | Read and write timeout, seconds |
| `--list` | — | Print all registered providers and exit |
| `--version` | — | Print version and exit |

### Secrets

Secrets are never passed on the command line. `--secret-env` names the environment
variable that holds the secret; the value is read from the environment.

```bash
export STRIPE_WEBHOOK_SECRET=whsec_...
webhookd stripe --secret-env STRIPE_WEBHOOK_SECRET
```

For providers with a conventional variable name, the subcommand supplies the
default, so the flag can be omitted:

```bash
export GITHUB_WEBHOOK_SECRET=...
webhookd github              # reads GITHUB_WEBHOOK_SECRET automatically
```

Config resolution order is **flag → environment → default**, so an explicit
`--secret-env` always wins.

### Examples

**GitHub, pipe pull request titles into a file:**

```bash
export GITHUB_WEBHOOK_SECRET=your_secret
webhookd github | jq -r 'select(.event=="pull_request") | .payload.pull_request.title' >> prs.txt
```

**Stripe, filter for successful payments and sum them:**

```bash
export STRIPE_WEBHOOK_SECRET=whsec_...
webhookd stripe \
  | jq -c 'select(.event=="payment_intent.succeeded") | .payload.data.object.amount' \
  | awk '{s+=$1} END {print s/100 " USD"}'
```

**Pretty mode for a live demo:**

```bash
webhookd github --pretty
```

```
github
──────────────────────────────────────
✓ Signature verified

Event:        pull_request
Delivery ID:  8a3f1b2c-...
Received:     2026-09-15T19:44:03Z

{
  "action": "opened",
  "number": 421
}
```

The header line is the provider's lowercase name. The core does not know how to
title-case `github` into `GitHub`, and it does not pretend to.

**Custom path behind a reverse proxy:**

```bash
webhookd stripe --path /webhooks/stripe --host 0.0.0.0 --port 9000
```

---

## Output

Every verified event produces exactly one JSON object on stdout, one per line.
The shape is identical across every provider:

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

| Field | Type | Description |
|---|---|---|
| `provider` | string | Lowercase provider name |
| `verified` | bool | Always `true` on stdout — failures never reach output |
| `event` | string | Provider-specific event type |
| `id` | string | Event ID if the provider supplies one, else `""` |
| `delivery_id` | string | Delivery or request ID if supplied, else `""` |
| `received_at` | string | ISO 8601 UTC timestamp of receipt |
| `payload` | object | Full JSON body, unmodified in structure and values |

All seven fields are always present. Fields without a value are emitted as `""`,
never omitted.

`payload` preserves key order, duplicate keys, and numeric precision exactly as
the provider sent them. The only change is whitespace normalisation: the value
is emitted compact on one line with insignificant whitespace removed and any
embedded newlines escaped, which is what makes the output line-oriented.

### stdout vs stderr

| Stream | Content |
|---|---|
| stdout | JSONL event lines, one per verified webhook. Nothing else. |
| stderr | Startup banner, errors, diagnostics, signature failures. |

This is the contract that makes piping work. If you ever see a non-JSON line on
stdout, that is a bug — please report it.

### Failed verification

An invalid signature produces a `401` to the sender and a line on stderr. Nothing
is written to stdout. The process keeps running and keeps listening.

```
webhookd: github: signature mismatch — 203.0.113.4
```

---

## HTTP responses

| Condition | Status | Body |
|---|---|---|
| Verified | `200 OK` | `{"ok": true}` |
| Invalid signature | `401 Unauthorized` | `{"error": "signature verification failed"}` |
| Unknown provider | `404 Not Found` | `{"error": "unknown provider"}` |
| Wrong method | `405 Method Not Allowed` | `{"error": "method not allowed"}` |
| Body too large | `413 Payload Too Large` | `{"error": "request body too large"}` |
| Bad content type | `415 Unsupported Media Type` | `{"error": "unsupported content type"}` |
| Malformed JSON body | `500 Internal Server Error` | `{"error": "internal error"}` |

### Health endpoint

```
GET /health
```

```json
{"status":"ok","version":"0.1.0"}
```

Always `200`. No dependency checks. No metrics.

---

## Security model

`webhookd` is designed to be safe by default on a developer machine and explicit
when exposed.

- **Bind address defaults to `127.0.0.1`.** Reaching it from outside requires
  `--host 0.0.0.0`, which is a deliberate choice.
- **Signature verification is constant-time.** Every provider uses `hmac.Equal()`.
- **Timestamps are enforced.** Providers that send a signed timestamp reject
  requests older than 300 seconds.
- **Body size is capped.** Default 2MB, enforced during the read via
  `http.MaxBytesReader` — an oversized body is never fully buffered.
- **Read and write timeouts are 10 seconds.** Connections cannot hang the process.
- **Secrets never appear on the command line.** Only the *name* of the environment
  variable is passed via `--secret-env`.

If you run `webhookd` on a public host, put it behind a reverse proxy that
terminates TLS, and set `--host 127.0.0.1` so only the proxy can reach it.

To report a vulnerability, see [SECURITY.md](SECURITY.md). Do not open a public issue.

---

## Build from source

**Requirements:** Go 1.25+, `make`, optionally `golangci-lint`.

```bash
git clone https://github.com/0xProgress/webhookd
cd webhookd
make build          # → bin/webhookd
make test           # go test -race -count=1 ./...
make check          # fmt + vet + lint + test — what CI runs
```

Cross-compile for all supported platforms with GoReleaser:

```bash
make release
```

Docker image:

```bash
docker build -t webhookd .
docker run --rm -p 8080:8080 -e GITHUB_WEBHOOK_SECRET=... webhookd github
```

---

## Contributing

The most impactful contribution is **adding a provider**. It is a well-scoped,
well-documented process:

1. Find or open the provider issue.
2. Read [CONTRIBUTING.md](CONTRIBUTING.md).
3. Implement against the `Provider` interface under `providers/<name>/`.
4. Add `cmd/<name>.go`, write tests, write the doc file, run `make check`.
5. Open a PR.

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you start. It covers the provider
process step by step, the commit message format, and the self-review checklist.

Small fixes — typos, doc clarifications, obvious bugs — can go straight to a PR.
Everything else should start with an issue.

---

## Design

`webhookd` is deliberately small. The core knows nothing about any specific provider.
It captures the raw body, hands it to a registered provider, and writes the result.

Three decisions shape everything else:

1. **Raw body first.** The bytes are captured before anything else touches the
   request and are never re-encoded. Verification and parsing see the same bytes,
   and `payload` preserves them — key order, duplicate keys, and numeric precision
   all survive to the consumer.
2. **stdout is data, stderr is everything else.** This is what makes `| jq` work
   without filters or `2>/dev/null`.
3. **Providers are leaf nodes.** A provider verifies a signature and extracts four
   strings. It does not decide what an event means. That is the caller's job.

Read [docs/architecture.md](docs/architecture.md) for the full picture.

---

## License

[MIT](LICENSE) © 0xProgress
