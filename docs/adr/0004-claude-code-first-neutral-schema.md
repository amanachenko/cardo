# ADR-0004 — Claude Code first, with a vendor-neutral canonical layer

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md`; `2026-09-21-prior-art-survey.md`.

## Context

Orgs genuinely run two or three coding agents at once, so "which agent for which work, at what cost" is
a real question. But Claude Code is unusually generous with telemetry — native OTel, ~30 hook events,
local transcripts — while several competitors expose far less. A schema built to the thinnest common
denominator would discard most of what makes Claude Code's signal valuable.

There is also no stable neutral standard available to adopt: `gen_ai.*` OpenTelemetry semantic
conventions were split into their own repository in June 2026 and have no tagged release. Claude Code
uses its own `claude_code.*` namespace with a partial `gen_ai.*` overlay.

## Decision

Support Claude Code only, in v1. Define a thin canonical event layer (ADR-0010) that is vendor-neutral
in shape, with a namespaced extension area for vendor-specific richness. Pre-built reports degrade
gracefully rather than being written to the lowest common denominator.

Do not build a second adapter until the first one is deployed somewhere real.

No product name, module path or table name may contain "Claude" — Anthropic trademark guidance, and it
would contradict this ADR (see ADR-0020).

## Consequences

- We define the neutral layer ourselves rather than adopting one. Mitigated by ADR-0010 making the
  canonical layer a set of SQL views rather than a storage format, so it can be rewritten cheaply once
  a second vendor's data is actually in hand.
- The vendor-neutrality promise stays a real commitment we can keep, instead of a design we had to get
  right blind.

## Rejected alternatives and why

- **Claude Code only, no pretense of portability.** Fastest, narrowest. Costs nothing now but makes the
  second adapter a rewrite instead of an addition.
- **Multi-agent from day one** (Cursor, Codex CLI, Gemini CLI, Copilot alongside). Risks a schema built
  to the thinnest data source, and ClawMetry has already done the unglamorous multi-runtime adapter work
  — competing on breadth is competing where we are weakest.
