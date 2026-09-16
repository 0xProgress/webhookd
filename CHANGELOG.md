# Changelog

All notable changes to webhookd are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Planned

- `github` provider — HMAC-SHA256 over the raw body, `X-Hub-Signature-256`
- `stripe` provider — HMAC-SHA256 over `{timestamp}.{body}`, 300s timestamp check
- `slack` provider — HMAC-SHA256 over `v0:{timestamp}:{body}`, 300s timestamp check
- `shopify` provider — base64-encoded HMAC-SHA256 over the raw body

Nothing is committed to any of the above. Each provider is a separate PR
against a frozen core; see [CONTRIBUTING.md](CONTRIBUTING.md).

---

## [0.1.0]

The first release. This is the core: a working webhook receiver that verifies
signatures, streams verified events as JSONL on stdout, and does nothing else.

### Added

**Core pipeline**

- HTTP listener with read and write timeouts of 10 seconds
- Raw request body captured once and never re-encoded — the same bytes are
  passed to signature verification and emitted as `payload`
- Body size limit of 2 MB, enforced during the read via `http.MaxBytesReader`
- Requests rejected with the correct status for every failure mode: 401 for a
  bad signature, 404 for an unknown provider, 405 for a non-POST method, 413
  for an oversized body, 415 for an unsupported content type, 500 for an
  internal error
- Health endpoint at `GET /health`
- Default bind address of `127.0.0.1`; binding to `0.0.0.0` requires an
  explicit `--host 0.0.0.0`

**Provider interface and registry**

- The `Provider` interface — five methods a provider implements to answer
  five questions about a request
- Provider registry with self-registration via `init()`
- Registry panics on duplicate provider names, catching a programming error
  at startup rather than at request time
- The interface is frozen at v0.1

**Output**

- JSONL event stream on stdout, one object per line, identical shape for every
  provider
- Output preserves key order, duplicate keys, and numeric precision from the
  request body — the payload is emitted from the raw bytes, not re-encoded
  from a decoded map
- Pretty mode (`--pretty`) for human-readable output during live demos
- All diagnostics — startup banner, errors, signature failures — on stderr;
  nothing but data on stdout

**CLI**

- `webhookd <provider>` to run a provider receiver
- `webhookd --list` to print registered providers
- `webhookd --version` to print the version
- Flags: `--secret-env`, `--port`, `--host`, `--path`, `--pretty`,
  `--max-body`, `--timeout`
- Configuration resolves in the order flag → environment → default
- Secrets are passed by name (`--secret-env STRIPE_WEBHOOK_SECRET`) and read
  from the environment; the value never appears on the command line

**Mock provider**

- A reference implementation of the `Provider` interface
- Accepts any request with the header `X-Mock-Signature: valid`
- Ships in every build; used by the server test suite
- Not intended for production

**Documentation**

- `README.md` with a five-minute demo that works end to end
- `CONTRIBUTING.md` covering the provider contribution process
- `docs/architecture.md` explaining the core/provider split
- `docs/webhookd-core.md` as the build specification
- `docs/providers/checklist.md` as a copy-pasteable self-review list
- `docs/providers/TEMPLATE.md` as the provider documentation template
- `SECURITY.md` with a vulnerability reporting process

**Distribution**

- Cross-compiled binaries for linux/amd64, linux/arm64, darwin/amd64,
  darwin/arm64, windows/amd64, windows/arm64
- Multi-architecture Docker image on `ghcr.io/0xprogress/webhookd`

### Security

- Signature comparison uses `hmac.Equal()` in every provider, avoiding timing
  side channels
- Timestamps in providers that use them are rejected if older than 300 seconds,
  protecting against replay
- Request bodies are capped at 2 MB, enforced before the body is fully read
- Secrets are read from environment variables, never from flags

---

[Unreleased]: https://github.com/0xProgress/webhookd/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/0xProgress/webhookd/releases/tag/v0.1.0