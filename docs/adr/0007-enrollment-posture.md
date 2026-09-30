# ADR-0007 — Thin mandatory core plus opt-in depth

**Status:** Accepted
**Date:** 2026-09-22
**Evidence:** `2026-09-20-claude-code-telemetry-surfaces.md` (managed settings can force telemetry and
prevent users overriding it); judgment for the posture.

## Context

Managed settings let an administrator force `CLAUDE_CODE_ENABLE_TELEMETRY=1`, pin the OTLP endpoint and
auth header, and set `allowManagedHooksOnly: true` so the hook pack cannot be disabled or replaced.
Precedence is managed > CLI > project > user. Rollout is therefore trivially enforceable — which is
exactly what makes it a trust hazard.

The tension: fully opt-in telemetry produces severe sampling bias (the engineers you most want to learn
from are the least likely to enroll), which makes fleet-level questions unanswerable. Fully mandatory
telemetry reads as surveillance and invites circumvention.

## Decision

A **thin mandatory core** plus **opt-in depth**.

Mandatory (T1): session started/ended, model, effort, token and cost, tool *classes*, skill names. No
content, no file paths, no prompts. The list is short, published in the README, and adding to it
requires a new ADR (INV-4).

Opt-in (T2): anything touching *what you were working on*. The engineer who opts in gets something
concrete back — their own dashboard (v0.2).

## Consequences

- The mandatory tier can honestly be framed as the same category of thing as an MDM hardware inventory.
- Avoids the sampling-bias trap of pure opt-in and the resentment of pure mandatory.
- The boundary is legible because the mandatory list is short enough to read in one sitting.
- **An org administrator can force full collection via managed settings regardless of what Cardo wants.**
  We cannot prevent that. The defence is that the config is human-readable and the project documents
  what a coercive deployment looks like, so engineers can check what was actually deployed.

## Rejected alternatives and why

- **Fully opt-in.** Maximum trust, useless fleet data. The people whose workflow problems you most need
  to see will not enroll.
- **Default-on with silent opt-out.** Honest, but fleet numbers quietly rot as people disable it and
  nobody knows the denominator changed.
- **Mandatory with no opt-out.** Technically available today via managed settings. Reads as surveillance
  and invites circumvention; loses more signal than it gains.
