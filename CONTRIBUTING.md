# Contributing to webhookd

Thanks for your interest in contributing. This document covers everything you need to know before opening a PR.

---

## Table of Contents

- [Ways to Contribute](#ways-to-contribute)
- [Before You Start](#before-you-start)
- [Development Setup](#development-setup)
- [Adding a Provider](#adding-a-provider)
- [Commit Messages](#commit-messages)
- [Pull Request Process](#pull-request-process)
- [Code Standards](#code-standards)
- [Security Issues](#security-issues)

---

## Ways to Contribute

**Adding a provider** — The most impactful contribution. See [Adding a Provider](#adding-a-provider) below. Open issues tagged `provider` are good starting points.

**Fixing a bug** — Open an issue first if one doesn't exist. Reference it in your PR.

**Improving docs** — Documentation PRs are always welcome. No issue needed for small fixes.

**Reporting a bug** — Open an issue. Include your webhookd version, provider, what you expected, and what happened.

**Security issues** — Do not open a public issue. See [Security Issues](#security-issues).

---

## Before You Start

**Open an issue first for any non-trivial change.** This applies to new features, significant refactors, and new providers. It avoids wasted effort if the direction doesn't fit the project.

For providers specifically: check that an issue exists and is open before starting. If it doesn't exist, open one. Someone may already be working on it.

**Small fixes** (typos, doc clarifications, obvious bugs) can go straight to a PR.

---

## Development Setup

**Requirements:**
- Go 1.25 or later
- `golangci-lint` — [install instructions](https://golangci-lint.run/usage/install/)
- `make`

**Clone and build:**

```bash
git clone https://github.com/0xProgress/webhookd
cd webhookd
make build
```

**Run tests:**

```bash
make test
```

**Run all checks (what CI runs):**

```bash
make check
```

This runs format, vet, lint, and test in sequence. All must pass before opening a PR.

**Run with the mock provider:**

```bash
./bin/webhookd mock
```

The mock provider accepts any request with the header `X-Mock-Signature: valid` and rejects everything else. Useful for testing the core pipeline.

---

## Adding a Provider

This is the full process for adding a new webhook provider. Read it completely before starting.

Providers live in this repository under `providers/<name>/`. There is no external provider mechanism — a new provider is a PR against this repo, not a separate module that users import.

### 1. Check the open issue

Find the issue for your provider (e.g. `provider: shopify`). Comment that you're working on it so no one else starts the same work.

### 2. Read the provider interface

Every provider implements this exactly:

```go
type Provider interface {
    Name() string
    Verify(r *http.Request, rawBody []byte) error
    EventType(r *http.Request, rawBody []byte) string
    DeliveryID(r *http.Request) string
    EventID(r *http.Request, rawBody []byte) string
}
```

Read `providers/provider.go` for the full documentation on each method. The full specification is in `docs/webhookd-core.md`.

### 3. Study the mock provider

`providers/mock/mock.go` is a minimal working implementation. Read it. Your provider follows the same structure.

### 4. Read the provider's official docs

Before writing code, read the provider's webhook documentation carefully. You need to understand:

- Which header carries the signature
- What format the signature is in
- What data is signed (just the body? body + timestamp? a composed string?)
- Whether a timestamp is included and how old requests are rejected
- Which header or body field carries the event type
- Which header carries a delivery or request ID

Link to the official docs in your provider's documentation file.

### 5. Create the provider files

```
providers/
└── <name>/
    ├── <name>.go
    └── <name>_test.go

docs/providers/
└── <name>.md

cmd/
└── <name>.go
```

### 6. Implement the provider

```go
package name

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "net/http"
    "os"

    "github.com/0xProgress/webhookd/providers"
)

func init() {
    providers.Register(&Provider{})
}

type Provider struct{}

func (p *Provider) Name() string {
    return "name"
}

func (p *Provider) Verify(r *http.Request, rawBody []byte) error {
    // Read the secret from environment
    secret := []byte(os.Getenv("PROVIDER_WEBHOOK_SECRET"))
    if len(secret) == 0 {
        return errors.New("name: PROVIDER_WEBHOOK_SECRET is not set")
    }

    // Get signature from header
    sig := r.Header.Get("X-Provider-Signature")
    if sig == "" {
        return errors.New("name: missing signature header")
    }

    // Compute expected signature
    mac := hmac.New(sha256.New, secret)
    mac.Write(rawBody)
    expected := hex.EncodeToString(mac.Sum(nil))

    // MUST use hmac.Equal — never == or strings.Compare
    if !hmac.Equal([]byte(sig), []byte(expected)) {
        return errors.New("name: signature mismatch")
    }

    return nil
}

func (p *Provider) EventType(r *http.Request, rawBody []byte) string {
    return r.Header.Get("X-Provider-Event")
}

func (p *Provider) DeliveryID(r *http.Request) string {
    return r.Header.Get("X-Provider-Delivery")
}

func (p *Provider) EventID(r *http.Request, rawBody []byte) string {
    // Parse from body if needed, or return ""
    return ""
}
```

Replace `name` with your provider's identifier (lowercase) and `PROVIDER` / `X-Provider-*` with the real names from the provider's docs.

**Rules that are not optional:**

- Use `hmac.Equal()` for all MAC comparisons. Never `==`. Never `strings.Compare`. Never `bytes.Equal`. `hmac.Equal` is constant-time. The others are not.
- If the provider sends a timestamp, reject requests older than 300 seconds.
- Return error strings prefixed with the provider name: `"shopify: ..."`.
- No external dependencies. A provider is standard-library-only.

### 7. Write tests

Your test file must cover at minimum:

```
✓ Valid signature is accepted
✓ Tampered body is rejected
✓ Wrong secret is rejected
✓ Missing signature header is rejected
✓ Timestamp too old is rejected (if provider uses timestamps)
✓ EventType returns correct value
✓ DeliveryID returns correct value (if applicable)
```

Use real HMAC test vectors where the provider's docs include them. Do not invent example values.

```go
package name

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "net/http/httptest"
    "testing"
)

func TestVerify_ValidSignature(t *testing.T) {
    // Use a known secret and body to compute a real signature,
    // then verify it. Do not hardcode a signature you made up.
    secret := "test-secret"
    body := []byte(`{"type":"test.event"}`)

    // Compute the signature the same way the provider does
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    sig := hex.EncodeToString(mac.Sum(nil))

    t.Setenv("PROVIDER_WEBHOOK_SECRET", secret)

    req := httptest.NewRequest("POST", "/provider", bytes.NewReader(body))
    req.Header.Set("X-Provider-Signature", sig)

    p := &Provider{}
    if err := p.Verify(req, body); err != nil {
        t.Fatalf("expected valid signature to pass, got: %v", err)
    }
}

func TestVerify_TamperedBody(t *testing.T) {
    secret := "test-secret"
    originalBody := []byte(`{"type":"test.event"}`)
    tamperedBody := []byte(`{"type":"test.event","amount":9999}`)

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(originalBody)
    sig := hex.EncodeToString(mac.Sum(nil))

    t.Setenv("PROVIDER_WEBHOOK_SECRET", secret)

    req := httptest.NewRequest("POST", "/provider", bytes.NewReader(tamperedBody))
    req.Header.Set("X-Provider-Signature", sig)

    p := &Provider{}
    if err := p.Verify(req, tamperedBody); err == nil {
        t.Fatal("expected tampered body to fail verification")
    }
}
```

Tests must use `testing` and `net/http/httptest` only. No assertion libraries. Assert on raw bytes where the output format is part of the contract, not on parsed structures.

### 8. Write the documentation file

Copy `docs/providers/TEMPLATE.md` and fill it in completely. Every section is required. The PR check will fail if sections are missing.

### 9. Add the subcommand

Add a file `cmd/<name>.go` that wires the provider into the CLI:

- Declares the provider subcommand
- Imports the provider package so its `init()` runs and it registers itself
- Sets any provider-specific defaults (for example, the conventional environment variable name for the signing secret, used when `--secret-env` is empty)

Follow the pattern of existing command files.

### 10. Self-review with the checklist

Before opening your PR, go through this:

```
Interface
[ ] Implements Name()
[ ] Implements Verify()       — uses hmac.Equal()
[ ] Implements EventType()
[ ] Implements DeliveryID()
[ ] Implements EventID()
[ ] Calls providers.Register() in init()
[ ] Has a cmd/<name>.go that imports the provider package

Security
[ ] hmac.Equal() used for all MAC comparisons
[ ] Timestamp validated and rejected if > 300s old (where applicable)
[ ] Empty/missing secret returns a clear error
[ ] Missing signature header returns a clear error
[ ] Error messages are prefixed with provider name

Tests
[ ] Valid signature accepted
[ ] Tampered body rejected
[ ] Wrong secret rejected
[ ] Missing header rejected
[ ] Timestamp too old rejected (where applicable)
[ ] EventType returns correct value
[ ] DeliveryID returns correct value (where applicable)
[ ] All tests pass: make test

Documentation
[ ] docs/providers/<name>.md exists
[ ] Signature Scheme section complete
[ ] Verification Steps section complete
[ ] Event Type section complete
[ ] Environment Variables section complete
[ ] Example Output section complete

Code quality
[ ] make check passes clean
[ ] No dependencies added to go.mod
[ ] No external packages imported by the provider
```

---

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add shopify provider
fix: correct hmac comparison in github provider
docs: add discord provider documentation
test: add timestamp validation tests for slack
refactor: simplify registry lookup
chore: update go version in workflows
```

The PR title must follow this format. The CI check will fail if it doesn't. The rules are enforced by `.commitlintrc.json` at the repository root — read it if you are unsure whether your commit will pass.

**Types:**
- `feat` — new provider or feature
- `fix` — bug fix
- `docs` — documentation only
- `test` — tests only, no code change
- `refactor` — code change with no behaviour change
- `chore` — maintenance (deps, config, CI)
- `ci` — changes to workflow files
- `perf` — performance improvement

---

## Pull Request Process

1. Fork the repo and create a branch: `feat/shopify-provider`
2. Make your changes
3. Run `make check` — all must pass
4. Open a PR with a clear title and description
5. Automated checks run — fix anything they report
6. Once all checks pass, the maintainer is tagged automatically
7. Review feedback comes as line comments — address each one
8. On approval, the maintainer merges

**PR description should include:**
- What this PR does
- Which issue it closes (`Closes #123`)
- How you tested it
- Link to the provider's official webhook documentation (for provider PRs)

---

## Code Standards

**Formatting:** `gofmt`. Run `make fmt` before committing.

**Imports:** Standard library only in provider implementations. No exceptions. The core's only external dependency is `github.com/spf13/cobra`, used in `cmd/` for CLI wiring.

**Errors:** Return errors, don't panic. Prefix with the package or provider name. The one exception is duplicate provider registration in `providers/registry.go`, which panics by design.

**Comments:** Public types and functions have doc comments. Interface methods get doc comments that describe the contract, not the implementation. Comments explain *why*, not *what*.

**Tests:** Use `testing` and `net/http/httptest`. No test framework dependencies.

**Constants:** Timeout values, size limits, and age limits are constants, not magic numbers.

---

## Security Issues

**Do not open a public issue for security vulnerabilities.**

Email the security contact listed in `SECURITY.md`.

Include:
- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Your assessment of impact

You'll receive a response within 48 hours. Security issues are treated as the highest priority.
\