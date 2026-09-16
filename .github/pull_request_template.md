## What this PR does

<!-- One paragraph. What changed and why. Be specific. -->

## Related issue

Closes #

## Type

- [ ] New provider
- [ ] Bug fix
- [ ] Documentation
- [ ] Refactor
- [ ] Chore / CI

## How I tested it

<!--
Commands you ran and output you observed.
For provider PRs: include the curl you used to send a test webhook.
Example:
  curl -X POST http://localhost:8080/github \
    -H "X-Hub-Signature-256: sha256=<computed>" \
    -H "X-GitHub-Event: push" \
    -H "Content-Type: application/json" \
    -d '{"ref":"refs/heads/main"}'
-->

---

## Provider PRs only

Skip this section if this is not a provider PR.

- [ ] An open issue existed before I started work (`provider: <name>` label)
- [ ] Official webhook documentation link:
- [ ] `docs/providers/<name>.md` written from `docs/providers/TEMPLATE.md`, every section filled
- [ ] All items on the self-review checklist in `CONTRIBUTING.md` are checked
- [ ] Provider code imports only standard-library packages
- [ ] Test vectors are real — taken from the provider's official docs, not fabricated
- [ ] Both valid-signature and tampered-body test cases are present

---

## General checklist

- [ ] `make check` passes locally with no errors
- [ ] PR title follows Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`, `ci:`)
- [ ] Nothing is written to stdout except through the JSONL event writer
- [ ] No secrets, tokens, or real webhook payloads are committed
