# ADR-0012 — Hook path stays pure collector config; one Go binary for the Admin API

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md`; OTel Collector contrib
(`webhookevent` receiver, OTTL `SHA256` and `env` converters).

## Context

ADR-0011 makes hooks arrive as HTTP POSTs. The OTel Collector contrib distribution has a `webhookevent`
receiver that turns arbitrary HTTP bodies into log records, and OTTL provides `SHA256()` and `env()`.
The pseudonymization contract from ADR-0006 is therefore expressible in configuration alone:

```
set(attributes["user.pseudonym"], SHA256(Concat([attributes["user.email"], env("CARDO_SALT")], "")))
delete_key(attributes, "user.email")
```

That would make the entire data path auditable YAML with no custom code touching anyone's events.

Separately, the Admin API tier (ADR-0017) needs a scheduled poller, which cannot be expressed as
collector configuration.

## Decision

Keep the **hook path as pure collector configuration** for as long as it holds. Ship **one small Go
binary** (`cardo`) for the Admin API poller and the SQL migration runner (ADR-0019).

Go rather than Python: a single static binary is dramatically easier for an organization's platform
team to accept than a Python environment on their collector host.

## Consequences

- "No code we wrote ever touches your events" is a claim worth real effort to preserve, and it holds for
  the hook path in v0.1.
- Zero build or release machinery is needed for the hook pipeline.
- **OTTL is expected to become unwieldy** across 13 differently-shaped hook payloads. That is the signal
  to write the receiver service — not a reason to write it now. See `risks.md` #4.
- Request authentication on the webhook endpoint is limited to what the receiver supports; revisit when
  the service is written.

## Rejected alternatives and why

- **Thin Go receiver service from the start.** Validation, HMAC auth, clean shaping and easy tests. Gives
  up the pure-config trust claim on day one for a problem we have not hit yet.
- **Pure collector config with no exceptions.** Maximum auditability; would mean no Admin API tier at
  all, since a scheduled poller cannot be configuration.
- **Python service.** Fastest to write and read; a Python environment on an organization's collector
  host is materially harder to get approved than a static binary.
