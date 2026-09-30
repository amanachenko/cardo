# ADR-0010 — Layered schema: raw verbatim, thin canonical views, marts

**Status:** Superseded by [ADR-0023](0023-hook-payload-allowlist.md)
**Date:** 2026-09-22
**Evidence:** `2026-09-21-prior-art-survey.md` (OTel GenAI convention status);
`2026-09-20-claude-code-telemetry-surfaces.md` (version-gated attributes).

## Context

There is no stable neutral schema to adopt. `gen_ai.*` semantic conventions were split into their own
repository in June 2026 with no tagged release. Claude Code emits under `claude_code.*` with a partial
`gen_ai.*` overlay, and several attributes are version-gated (`2.1.214+`, `2.1.269+`). More will appear.

So the choice is between guessing a neutral schema now and being lossy, or storing everything and
pushing complexity onto every consumer.

## Decision

Three layers:

- **bronze** — raw `claude_code.*` events and hook payloads, persisted verbatim, nothing dropped
- **silver** — approximately six canonical event types, expressed as SQL views over bronze:
  `session`, `turn`, `tool_call`, `policy_decision`, `artifact_load`, `context_event`
- **gold** — marts: artifact analytics, friction index, cost and capacity

Keep the canonical layer deliberately small — about six event types, not sixty.

## Consequences

- A Claude Code release that adds attributes never loses us data, and new derived fields can be
  backfilled over the entire history.
- The neutral schema does not have to be guessed before a second vendor's data is in hand: the canonical
  layer is a set of views we can rewrite, not a storage format we are married to. This is what makes
  ADR-0004's vendor-neutrality promise cheap to keep.
- Version drift breaks only derived views, never ingest (ADR-0014).
- Bronze grows faster than a normalized store would. Accepted; ClickHouse compresses it well and
  retention (ADR-0016) bounds it.

## Rejected alternatives and why

- **Canonicalize at ingest** — define the neutral event model now, map at the collector, store only the
  canonical form. Clean, and lossy the first time an unanticipated attribute ships. Unrecoverable,
  because the raw event is gone.
- **Store raw only**, normalize in every query. Never loses data; pushes all the complexity onto every
  consumer and makes the pre-built reports unmaintainable.
