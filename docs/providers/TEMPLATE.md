# [Provider Name] — webhookd Provider

> One sentence describing what [Provider Name] is and what events it sends.

**Official docs:** [Link to provider's webhook documentation]

---

## Signature Scheme

Describe exactly how this provider signs webhook requests.

| Detail | Value |
|--------|-------|
| Signature header | `X-Provider-Signature` |
| Signature format | `sha256=<hex_digest>` or `v0=<hex_digest>` etc. |
| Algorithm | HMAC-SHA256 |
| What is signed | Raw request body / `{timestamp}.{body}` / etc. |
| Timestamp header | `X-Provider-Timestamp` (or "None") |
| Max timestamp age | 300 seconds (or "Not applicable") |

---

## Verification Steps

Numbered, exact steps matching the implementation in `<name>.go`.

1. Read the `X-Provider-Signature` header
2. Read the `X-Provider-Timestamp` header (if applicable)
3. Reject if timestamp is older than 300 seconds (if applicable)
4. Construct the signed string: `{timestamp}.{rawBody}` (or just `rawBody`)
5. Compute `HMAC-SHA256(secret, signedString)`
6. Compare result with the signature header value using constant-time comparison
7. Reject with 401 if comparison fails

---

## Event Type

Where the event type is found and what format it takes.

| Field | Source | Example value |
|-------|--------|---------------|
| Event type | `X-Provider-Event` header or `type` field in body | `push`, `payment.succeeded` |

---

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `PROVIDER_WEBHOOK_SECRET` | Yes | The signing secret from your provider dashboard |

---

## Usage

```bash
export PROVIDER_WEBHOOK_SECRET=your_secret_here

webhookd <name>
```

Default endpoint: `POST //<name>`

Custom path:

```bash
webhookd <name> --path /webhooks/<name>
```

---

## Example Output

A complete example of the JSONL line produced for a real event from this provider.

```json
{
  "provider": "<name>",
  "verified": true,
  "event": "example.event",
  "id": "evt_example123",
  "delivery_id": "delivery_abc456",
  "received_at": "2026-09-15T19:42:13Z",
  "payload": {
    "type": "example.event",
    "data": {
      "example": "value"
    }
  }
}
```

---

## Notes

Any provider-specific gotchas, edge cases, or things to be aware of.

For example:
- Some events from this provider do not include a delivery ID
- The timestamp is in Unix seconds, not milliseconds
- URL-verification challenges should be handled separately (Slack)

---

## References

- [Provider Webhook Documentation](https://example.com/docs/webhooks)
- [Signature Verification Guide](https://example.com/docs/webhooks/signatures)
- [Event Types Reference](https://example.com/docs/webhooks/events)
