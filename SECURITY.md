# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | ✅        |
| older   | ❌        |

Only the latest release receives security fixes.

## Reporting a Vulnerability

**Do not open a public issue.**

Report vulnerabilities privately using GitHub's security advisory system:
👉 https://github.com/0xProgress/webhookd/security/advisories/new

Include:
- A description of the vulnerability
- Steps to reproduce it
- Affected versions
- Your assessment of the impact

You will receive a response within 48 hours. Critical issues are treated as the
highest priority and will be patched and released as quickly as possible.

## Scope

webhookd handles externally supplied HTTP traffic and performs cryptographic
signature verification. The following are always in scope:

- Signature verification bypass
- Timing attacks on MAC comparison
- Denial of service via malformed requests
- Secret exposure via logging or output
- Dependency vulnerabilities in the core binary

## Out of Scope

- Issues requiring the attacker to already have the webhook secret