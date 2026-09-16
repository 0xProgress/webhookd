# Provider self-review checklist

> This is a companion to the canonical checklist in
> [CONTRIBUTING.md §"Adding a Provider", step 10](../../CONTRIBUTING.md#10-self-review-with-the-checklist).
>
> `CONTRIBUTING.md` is the source of truth. If this file and that one ever
> disagree, `CONTRIBUTING.md` wins. This file exists so you can open it in a
> second window, paste it into a PR description, or print it.

Copy the checklist below into your PR description and tick each box. Every
box must be checked before the PR is ready for review.

---

## Pre-work

- [ ] An open issue exists for this provider (`provider: <name>` label)
- [ ] I have commented on the issue to claim it
- [ ] I have read the provider's official webhook documentation
- [ ] I have read [CONTRIBUTING.md §"Adding a Provider"](../../CONTRIBUTING.md#adding-a-provider) end to end
- [ ] I have read [docs/contributing/provider-guide.md](provider-guide.md)
- [ ] I have read [providers/provider.go](../../providers/provider.go) — the interface doc comments

---

## Interface

- [ ] `Name()` implemented — returns the lowercase identifier
- [ ] `Verify(r, rawBody)` implemented — uses `hmac.Equal()`
- [ ] `EventType(r, rawBody)` implemented
- [ ] `DeliveryID(r)` implemented
- [ ] `EventID(r, rawBody)` implemented
- [ ] `providers.Register()` called in `init()`
- [ ] `cmd/<name>.go` added and imports the provider package

---

## Security

- [ ] `hmac.Equal()` used for all MAC comparisons
- [ ] No `==`, `strings.Compare`, or `bytes.Equal` on signature material
- [ ] Raw body is hashed directly — no `json.Unmarshal` before hashing
- [ ] Timestamp validated and rejected if older than 300 seconds (where applicable)
- [ ] Timestamp validated *after* the signature check, not before (where applicable)
- [ ] Empty or missing secret returns a clear error
- [ ] Missing signature header returns a clear error
- [ ] Malformed signature header returns a clear error
- [ ] Error messages are prefixed with the provider name (`"<name>: ..."`)
- [ ] Secret is read from the environment in `Verify()`, not cached in `init()` or on the struct
- [ ] Secret, signature, and body are never logged

---

## Tests

- [ ] Valid signature is accepted
- [ ] Tampered body is rejected
- [ ] Wrong secret is rejected
- [ ] Missing signature header is rejected
- [ ] Malformed signature header is rejected
- [ ] Timestamp too old is rejected (where applicable)
- [ ] `EventType` returns the correct value for the provider's events
- [ ] `DeliveryID` returns the correct value (where applicable)
- [ ] `EventID` returns the correct value (where applicable)
- [ ] Test vectors are real — taken from the provider's official docs, not fabricated
- [ ] Test computes the signature the same way the provider does, rather than hardcoding a value
- [ ] `testing` and `net/http/httptest` only — no assertion libraries
- [ ] All tests pass: `make test`

---

## Documentation

- [ ] `docs/providers/<name>.md` exists
- [ ] Copied from [docs/providers/TEMPLATE.md](../providers/TEMPLATE.md)
- [ ] Every section filled in — no placeholders left
- [ ] Signature Scheme table complete
- [ ] Verification Steps numbered, matching the implementation
- [ ] Event Type section complete
- [ ] Environment Variables section complete
- [ ] Example Output section contains a real JSONL line for a real event
- [ ] Official docs URL linked in the header
- [ ] Notes section covers any provider-specific gotchas

---

## Code quality

- [ ] `make check` passes clean (fmt + vet + lint + test)
- [ ] Provider code imports only standard-library packages
- [ ] No new `require` entries in `go.mod`
- [ ] No panics in `Verify()` or any request-path method
- [ ] Comments explain *why*, not *what*
- [ ] Public types and methods have doc comments
- [ ] No `// TODO` or `// FIXME` in place of logic
- [ ] No dead code, no commented-out code

---

## PR

- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) — `feat: add <name> provider`
- [ ] PR description includes the issue reference (`Closes #123`)
- [ ] PR description includes a `curl` example used to test the provider
- [ ] PR description links to the provider's official webhook documentation
- [ ] No secrets, tokens, or real webhook payloads anywhere in the diff
- [ ] Three files added: `providers/<name>/<name>.go`, `providers/<name>/<name>_test.go`, `cmd/<name>.go`
- [ ] Plus one doc file: `docs/providers/<name>.md`

---

## What to do if a box cannot be ticked

Do not leave a box unchecked and open the PR anyway. Every box on this list
exists because a previous contribution got that thing wrong, and the review
process will catch it — asking you to fix it after the PR is open.

If a box genuinely does not apply to your provider — for example, "timestamp
validated" for a provider that does not send timestamps — write **N/A** next
to it with a one-line reason. A reviewer who sees `N/A — provider sends no
timestamp header` does not need to open the code to know the box was
considered.

If a box *should* apply but cannot be ticked, that is a signal that either:

- the provider implementation is incomplete, or
- the box describes a rule the provider genuinely cannot satisfy, which is a
  spec question worth raising as an issue before opening the PR.

Do not open the PR "so the reviewer can tell me what's missing." That wastes
both of your time. The reviewer's job is reviewing, not debugging.