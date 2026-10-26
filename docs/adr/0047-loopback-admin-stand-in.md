# ADR-0047 — A loopback stand-in for the Analytics API, for verification only

**Status:** Accepted — **built**
**Date:** 2026-10-18
**Depends on:** ADR-0021 (pinned analytics endpoint), ADR-0036 (service runs)

## Context

The Admin API path cannot be verified without an organization admin key. That left three questions
open in testing: does the adapter parse the documented envelope, are both `user_actor` and
`api_actor` mapped correctly, and does an undocumented actor type surface as drift instead of being
silently counted. Fixtures answer the first two on synthetic bytes, but they do not exercise the
poller, the scrubber, and ClickHouse together, and they cannot be pointed at by a running binary.

## Decision

1. **A stand-in server:** `cardo mock-admin` serves
   `/v1/organizations/usage_report/claude_code` on loopback with the documented envelope, a
   configurable number of `user_actor` and `api_actor` records, and an optional undocumented type.
2. **A guarded base URL:** `cardo poll --base-url` accepts only `http(s)` URLs whose host is
   `127.0.0.1`, `::1`, or `localhost`. Anything else is refused with the reason "the Admin API
   endpoint is pinned so the key cannot be sent elsewhere". The default endpoint stays pinned.
3. **The stand-in ships in the binary, but the redirect does not:** an operator can point the
   poller at their own loopback service; they cannot point it at a remote host, so a hostile
   `CARDO_*` environment cannot exfiltrate the key.

## Consequences

- The full path — HTTP, pagination shape, scrubbing, pseudonymisation, ClickHouse insert, fleet
  view — is exercised locally with a real binary and a real database.
- The documented actor types are proven end to end, and an unknown type makes `cardo check` fail
  with its name.
- What it cannot prove: the real service's authentication, pagination cursors, rate limits, and any
  field Anthropic ships that the documentation does not describe. Those still need one live key.
- Every stand-in row is synthetic and must be deleted by `org_id` after a run; the verification step
  in the README does exactly that.
