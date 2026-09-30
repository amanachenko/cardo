# ADR-0011 — Hook transport is native async HTTP; zero local binary

**Status:** Superseded by [ADR-0024](0024-bundle-configures-telemetry-only.md)
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md`.

## Context

Claude Code supports `type: "http"` hooks with `async: true`, and managed settings can pin
`allowedHttpHookUrls` to a specific collector endpoint. This means the hook pack can deliver events with
**nothing installed on the developer machine** — the entire client side becomes a settings file.

The alternative transports all reintroduce an installed component.

## Decision

Native async HTTP hooks, posting to the org-edge collector. `allowedHttpHookUrls` pinned in managed
settings. No binary, daemon or script on developer machines.

A local-spool fallback is a known, deliberately deferred upgrade path — build it when an
organization asks, not before.

## Consequences

- "There is no agent — the entire client side is a readable config file" is an unusually strong trust
  property, and it is the one engineers can verify themselves in thirty seconds.
- Install collapses to an MDM/GPO/Intune push, which an admin can do in an afternoon. That is the
  difference between a pilot happening and not happening.
- **Events are lost when a laptop is offline or off-VPN.** Accepted: this is fleet statistics, not
  billing, and a few percent loss from offline machines does not change any conclusion drawn from it.
  The README must say so rather than implying completeness.
- A network call sits on hot-path events. `async: true` makes it fire-and-forget, but hook latency
  remains something to watch during the first deployment.

## Rejected alternatives and why

- **HTTP primary with local spool fallback.** No data loss, survives offline laptops. Reintroduces an
  installed component — the thing engineers squint at and the thing that breaks. Correct eventual
  answer, wrong first answer.
- **Command hooks writing to a local buffer, flushed on reconnect.** Reliable delivery, but there is now
  an agent on every laptop from day one.
- **Command hooks writing to a local OTel Collector.** Standard and robust; a real daemon to install and
  keep running on every machine.
