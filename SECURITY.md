# Security Policy

webhookd verifies webhook signatures. A vulnerability in signature verification
is a vulnerability in the only security control the tool provides. Security
reports are the highest priority.

---

## Supported versions

Only the latest minor release receives security fixes. Pre-release versions
are not supported.

| Version | Supported |
|---------|-----------|
| latest `v0.x` | ✅ |
| older `v0.x` | ❌ |
| pre-release | ❌ |

If you are running an older version, upgrade before reporting unless the report
is about a version-specific regression.

---

## Reporting a vulnerability

**Do not open a public issue.**

Use GitHub's private vulnerability reporting:

https://github.com/0xProgress/webhookd/security/advisories/new

If you cannot use GitHub, email the maintainer at the address listed on the
GitHub profile for `0xProgress`.

### What to include

- A description of the vulnerability and its impact
- Steps to reproduce, ideally as a `curl` command or a minimal Go test
- Affected versions
- Any proof-of-concept code
- Your assessment of severity and exploitability

The more concrete the reproduction, the faster the fix. A working test case
that fails against the current release is worth more than a paragraph of
description.

---

## Response timeline

| Stage | Time |
|-------|------|
| Acknowledgement of report | within 48 hours |
| Initial assessment and severity classification | within 5 days |
| Fix or mitigation available | depends on severity, typically within 14 days for high severity |
| Public disclosure | coordinated with the reporter, typically after a patched release |

If a report is out of scope or not reproducible, you will be told why.

---

## Scope

### In scope

- **Signature verification bypass.** Any way to make `Verify()` return nil for
  a request whose signature is invalid, missing, or forged.
- **Timing side channels in MAC comparison.** Any non-constant-time comparison
  of a secret-dependent value, or any observable difference in response time
  or error content that leaks information about a valid signature.
- **Replay attacks.** Requests with valid signatures but timestamps outside
  the accepted window being accepted by a provider that claims to enforce the
  window.
- **Denial of service via resource exhaustion.** Unbounded memory growth,
  unbounded goroutine creation, or unbounded CPU consumption triggered by a
  single request or a small number of requests.
- **Secret leakage.** Any code path that writes the signing secret, the
  computed MAC, or the raw signature to stdout, stderr, or an HTTP response.
- **Remote code execution.** Any way to cause webhookd to execute
  attacker-controlled code.
- **Path traversal or injection.** Any way to cause webhookd to read or write
  files outside its working directory, or to execute commands via request
  content.

### Out of scope

- **Vulnerabilities in a specific provider's upstream signature scheme.**
  If Stripe's documented scheme is flawed, that is a Stripe issue. If
  webhookd implements Stripe's scheme incorrectly, that is in scope.
- **Misconfiguration by the operator.** Binding to `0.0.0.0`, running without
  a secret, or exposing the port to the public internet without a reverse
  proxy are documented risks, not vulnerabilities.
- **Denial of service via legitimate traffic volume.** webhookd is not
  designed to withstand a volumetric attack. Put it behind a proxy.
- **Missing security headers on responses.** webhookd returns JSON to a
  webhook sender, not to a browser. Headers like `X-Frame-Options` and CSP
  are not applicable.
- **TLS.** webhookd does not terminate TLS. It is designed to run behind a
  reverse proxy that does. Running it without a proxy on an untrusted network
  is a deployment error.
- **Timing attacks on non-secret values.** Only comparisons involving the
  secret or the computed MAC are in scope.

If you are unsure whether something is in scope, report it privately anyway.
It is better to triage a report that turns out to be out of scope than to miss
a real issue because the reporter assumed it was not our problem.

---

## Security design notes

These are the properties webhookd is designed to hold. A report that shows one
of these failing is a valid vulnerability.

### Constant-time comparison

Every MAC comparison uses `hmac.Equal()`. This is a hard rule, not a
preference. `hmac.Equal` is constant-time with respect to the contents of the
compared values. `==`, `strings.Compare`, and `bytes.Equal` are not, and
short-circuit on the first differing byte, which leaks information about how
many bytes of a forged signature were correct.

This rule is enforced by code review and by the provider checklist in
`CONTRIBUTING.md`. It is not enforceable at runtime because the core cannot
see inside a provider's `Verify()` implementation.

### Timestamp validation

Providers whose upstream scheme includes a signed timestamp reject requests
older than 300 seconds. The window is deliberately short. A longer window
increases the replay surface; a shorter one increases the false-rejection rate
on networks with clock skew. 300 seconds matches the recommendation of most
providers and is the value used by Stripe and Slack.

Providers whose scheme has no timestamp cannot enforce freshness. This is a
limitation of the upstream scheme, not a webhookd bug, and is noted in each
provider's documentation.

### Body size limit

The request body is capped at 2MB by default, enforced before the body is
read. The cap is configurable via `--max-body` but the default is chosen so
that a single request cannot exhaust memory on a modest machine. The limit is
enforced with `http.MaxBytesReader`, which returns an error to the reader
rather than silently truncating.

### Timeouts

Read and write timeouts default to 10 seconds. A connection that does not
complete a request within the read timeout is closed. This prevents a slow or
stalled client from holding a connection open indefinitely.

### Bind address

The default bind address is `127.0.0.1`. Reaching webhookd from another host
requires explicitly passing `--host 0.0.0.0`, which is a deliberate
affirmation that the operator understands the exposure. This is the opposite
of the convention followed by most HTTP servers, which default to `0.0.0.0`
and require the operator to lock them down. The default here is chosen so that
the insecure configuration is the one that requires explicit action.

### Secret handling

Secrets are read from environment variables. The CLI takes the *name* of the
environment variable, never the value:

```bash
export STRIPE_WEBHOOK_SECRET=whsec_...
webhookd stripe --secret-env STRIPE_WEBHOOK_SECRET
```

This means the secret never appears in `ps`, in shell history, or in process
listings. It also means a misconfigured invocation fails loudly — an unset
variable produces a clear error rather than silently verifying against an
empty secret.

### No secret in logs

webhookd does not log the secret, the signature, or the request body. Error
messages from failed verification describe the failure ("signature mismatch")
without including the values involved.

---

## Hardening guidance for operators

webhookd's defaults are safe for a developer machine. Running it in
production requires additional care that is outside the tool's scope.

**Run behind a reverse proxy.** Terminate TLS at the proxy and set
`--host 127.0.0.1` so only the proxy can reach webhookd. Do not expose
webhookd directly to the internet.

**Restrict the proxy to the provider's source IPs.** Most providers publish
their webhook source ranges. Allow only those. Signature verification is the
last line of defense, not the first.

**Run as an unprivileged user.** webhookd needs no special privileges. It
binds to a high port by default. Use a systemd unit or container with a
non-root user.

**Set `--max-body` to the smallest value that works.** 2MB is a generous
default. If your provider's largest event is 50KB, set `--max-body 65536`.

**Do not pipe to anything that writes to disk without a rotation policy.**
webhookd will happily fill a disk if nothing is consuming its output.

**Keep the binary up to date.** Signature schemes change. A provider that was
correct six months ago may need a fix today.

---

## Disclosure policy

Security fixes are released as patch versions with a security advisory. The
advisory includes the affected versions, the fix version, a description of the
issue, and credit to the reporter unless they request anonymity.

Reporters are asked to keep the issue private until a fix is available. If a
fix cannot be produced within a reasonable timeframe, the reporter is free to
disclose after notifying the maintainer.

---

## Acknowledgements

Reporters who wish to be credited are listed in the advisory for the issue
they reported. There is no bug bounty.
