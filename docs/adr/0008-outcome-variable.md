# ADR-0008 — The outcome variable is a friction index

**Status:** Superseded by [ADR-0031](0031-what-works-means.md)
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md` for signal availability. The choice itself
is judgment and is **explicitly unvalidated** — see `risks.md` #1.

## Context

ADR-0001 makes the product ask whether sessions using an enablement artifact "look different." That
requires defining *different*. Without an outcome variable the artifact-centric frame degenerates into
raw usage counts, which answer whether a skill is invoked but not whether it helps.

## Decision

A three-component **friction index**, computable entirely from OTel and hook data with no external
integration:

- **edit rejection rate** — from the native `claude_code.code_edit_tool.decision` metric
- **permission-wall time** — derived from `PermissionRequest` to decision hook timestamps
- **compaction rate** — compactions per session, from `PreCompact`/`PostCompact`

The schema is designed so that delivery outcomes can join later via commit SHA (ADR-0010), but that
integration is not built.

## Consequences

- Actionable in a way a DORA metric is not: "this skill's sessions have 40% fewer rejected edits —
  promote it" is a decision a platform team can act on this week.
- Requires no git, Jira or CI integration, so v0.1 has no external dependencies.
- **Friction is a proxy for value, not value.** A session with zero friction may be a session that
  accomplished nothing. **Any dashboard built on this must show a volume denominator beside it, or it
  will reward timidity.** This constraint is not optional.
- Permission-wall time is derived from hook timestamps rather than the cleaner `tool.blocked_on_user`
  span, because that span requires `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`. Slightly noisier, but on
  stable surfaces. Add the trace path as optional enrichment once the beta stabilizes.

## Rejected alternatives and why

- **Delivery outcomes** (cycle time, rework on agent-authored lines, revert rate, via a git/PR join).
  What orgs actually care about. Also a separate integration project, and precisely what DX, Faros and
  Jellyfish charge for — a crowded paid market to enter on day one.
- **Self-reported micro-surveys.** Real ground truth, but survey fatigue is real and this is essentially
  DX's entire business.
- **No single outcome variable** — expose distributions and let each admin correlate. Honest, but leaves
  the hardest work to the user and weakens every pre-built report.
